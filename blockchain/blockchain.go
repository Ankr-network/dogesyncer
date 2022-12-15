package blockchain

import (
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/go-hclog"
	lru "github.com/hashicorp/golang-lru"
	"github.com/sunvim/dogesyncer/chain"
	"github.com/sunvim/dogesyncer/contracts/systemcontracts"
	"github.com/sunvim/dogesyncer/contracts/upgrader"
	"github.com/sunvim/dogesyncer/contracts/validatorset"
	"github.com/sunvim/dogesyncer/crypto"
	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/sunvim/dogesyncer/helper/common"
	"github.com/sunvim/dogesyncer/rawdb"
	"github.com/sunvim/dogesyncer/state"
	itrie "github.com/sunvim/dogesyncer/state/immutable-trie"
	"github.com/sunvim/dogesyncer/types"
	"github.com/sunvim/dogesyncer/types/buildroot"
)

type Blockchain struct {
	logger   hclog.Logger
	config   *chain.Chain
	chaindb  ethdb.Database
	state    *itrie.State
	genesis  types.Hash
	stream   *eventStream // Event subscriptions
	executor Executor

	currentHeader     atomic.Value // The current header
	currentDifficulty atomic.Value // The current difficulty of the chain (total difficulty)
	stopped           atomic.Bool
	wg                *sync.WaitGroup

	headersCache         *lru.Cache // LRU cache for the headers
	blockNumberHashCache *lru.Cache // LRU cache for the CanonicalHash
	difficultyCache      *lru.Cache // LRU cache for the difficulty

	gpAverage *gasPriceAverage // A reference to the average gas price

}

func (b *Blockchain) Config() *chain.Chain {
	return b.config
}

// gasPriceAverage keeps track of the average gas price (rolling average)
type gasPriceAverage struct {
	sync.RWMutex

	price *big.Int // The average gas price that gets queried
	count *big.Int // Param used in the avg. gas price calculation
}

type Executor interface {
	BeginTxn(parentRoot types.Hash, header *types.Header, coinbase types.Address) (*state.Transition, error)
	//nolint:lll
	ProcessTransactions(transition *state.Transition, gasLimit uint64, transactions []*types.Transaction) (*state.Transition, error)
	Stop()
}

func NewBlockchain(logger hclog.Logger, db ethdb.Database, chain *chain.Chain, executor Executor, state *itrie.State) (*Blockchain, error) {
	b := &Blockchain{
		logger:   logger.Named("blockchain"),
		chaindb:  db,
		state:    state,
		config:   chain,
		stream:   &eventStream{},
		executor: executor,
		wg:       &sync.WaitGroup{},
		gpAverage: &gasPriceAverage{
			price: big.NewInt(0),
			count: big.NewInt(0),
		},
	}

	err := b.initCaches(32)
	if err != nil {
		return nil, err
	}

	b.stream.push(&Event{})
	return b, nil
}

func (b *Blockchain) Close() error {
	b.executor.Stop()
	b.stop()
	b.wg.Wait()

	return b.chaindb.Close()
}

func (b *Blockchain) stop() {
	b.stopped.Store(true)
}

func (b *Blockchain) isStopped() bool {
	return b.stopped.Load()
}

func (b *Blockchain) SelfCheck() {
	var newheader *types.Header

	latest, _ := rawdb.ReadHeadHash(b.chaindb)
	header, _ := rawdb.ReadHeader(b.chaindb, latest)
	_, exist := rawdb.ReadCanonicalHash(b.chaindb, header.Number)
	if !exist { // missing latest header
		newheader, _ = b.GetHeaderByNumber(header.Number - 1)
	}

	// issue: when restart , missing state
	for index := header.Number; index > 0; index-- {
		newheader, _ = b.GetHeaderByNumber(index)
		_, err := rawdb.ReadState(b.chaindb, newheader.StateRoot)
		if err == nil {
			break
		}
	}
	rawdb.WriteHeadHash(b.chaindb, newheader.Hash)
	rawdb.WriteHeadNumber(b.chaindb, newheader.Number)
}

func (b *Blockchain) CurrentTD() *big.Int {
	td, ok := b.currentDifficulty.Load().(*big.Int)
	if !ok {
		return nil
	}

	return td
}

func (b *Blockchain) GetTD(hash types.Hash) (*big.Int, bool) {
	return b.readTotalDifficulty(hash)
}

// get receitps by block header hash
func (b *Blockchain) GetReceiptsByHash(hash types.Hash) ([]*types.Receipt, error) {
	// read body
	bodies, err := rawdb.ReadBody(b.chaindb, hash)
	if err != nil {
		return nil, err
	}
	// read receipts
	receipts := make([]*types.Receipt, len(bodies))
	for i, tx := range bodies {
		receipt, err := rawdb.ReadReceipt(b.chaindb, tx)
		if err != nil {
			return nil, err
		}
		receipts[i] = receipt
	}

	return receipts, nil
}

func (b *Blockchain) GetBodyByHash(hash types.Hash) (*types.Body, bool) {
	// read body
	bodies, err := rawdb.ReadBody(b.chaindb, hash)
	if err != nil {
		return nil, false
	}
	// read transactions
	txes := make([]*types.Transaction, len(bodies))
	for i, txhash := range bodies {
		tx, err := rawdb.ReadTransaction(b.chaindb, txhash)
		if err != nil {
			return nil, false
		}
		txes[i] = tx
	}
	return &types.Body{
		Transactions: txes,
	}, false
}

func (b *Blockchain) GetHeaderByHash(hash types.Hash) (*types.Header, bool) {
	header, err := rawdb.ReadHeader(b.chaindb, hash)
	if err != nil {
		return nil, false
	}
	return header, true
}

func (b *Blockchain) GetHeaderByNumber(n uint64) (*types.Header, bool) {
	// read hash
	hash, ok := rawdb.ReadCanonicalHash(b.chaindb, n)
	if !ok {
		return nil, false
	}

	header, err := rawdb.ReadHeader(b.chaindb, hash)
	if err != nil {
		return nil, false
	}

	return header, true
}

func (b *Blockchain) WriteBlock(block *types.Block) error {
	if b.isStopped() {
		return ErrClosed
	}
	b.wg.Add(1)
	defer b.wg.Done()

	// nil checked by verify functions
	header := block.Header

	// Log the information
	b.logger.Info("write block", "num", block.Number(), "parent", block.ParentHash())

	// write body
	if err := b.writeBody(block); err != nil {
		return err
	}

	blockResult, err := b.executeBlockTransactions(block)
	if err != nil {
		return err
	}

	if buildroot.CalculateReceiptsRoot(blockResult.Receipts) != header.ReceiptsRoot {
		return fmt.Errorf("mismatch receipt root %s != %s", header.ReceiptsRoot, blockResult.Root)
	}

	if buildroot.CalculateTransactionsRoot(block.Transactions) != header.TxRoot {
		return fmt.Errorf("mismatch receipt root %s != %s", header.ReceiptsRoot, blockResult.Root)
	}

	if blockResult.Root != header.StateRoot {
		return fmt.Errorf("mismatch state root %s != %s", header.StateRoot, blockResult.Root)
	}

	err = rawdb.WrteReceipts(b.chaindb, blockResult.Receipts)
	if err != nil {
		return err
	}

	// Write the header to the chain
	header.ComputeHash()
	if err := b.WriteHeader(header); err != nil {
		return err
	}

	// Update the average gas price
	b.updateGasPriceAvgWithBlock(block)

	logArgs := []interface{}{
		"number", header.Number,
		"hash", header.Hash,
		"txns", len(block.Transactions),
	}

	if prevHeader, ok := b.GetHeaderByNumber(header.Number - 1); ok {
		diff := header.Timestamp - prevHeader.Timestamp
		logArgs = append(logArgs, "generation_time_in_seconds", diff)
	}

	b.logger.Info("new block", logArgs...)

	return nil
}

// updateGasPriceAvgWithBlock extracts the gas price information from the
// block, and updates the average gas price for the chain accordingly
func (b *Blockchain) updateGasPriceAvgWithBlock(block *types.Block) {
	if len(block.Transactions) < 1 {
		// No transactions in the block,
		// so no gas price average to update
		return
	}

	gasPrices := make([]*big.Int, len(block.Transactions))
	for i, transaction := range block.Transactions {
		gasPrices[i] = transaction.GasPrice
	}

	b.updateGasPriceAvg(gasPrices)
}

// updateGasPriceAvg updates the rolling average value of the gas price
func (b *Blockchain) updateGasPriceAvg(newValues []*big.Int) {
	b.gpAverage.Lock()
	defer b.gpAverage.Unlock()

	//	Sum the values for quick reference
	sum := big.NewInt(0)
	for _, val := range newValues {
		sum = sum.Add(sum, val)
	}

	// There is no previous average data,
	// so this new value set will instantiate it
	if b.gpAverage.count.Uint64() == 0 {
		b.calcArithmeticAverage(newValues, sum)

		return
	}

	// There is existing average data,
	// use it to generate a new average
	b.calcRollingAverage(newValues, sum)
}

// calcRollingAverage calculates the new average based on the
// moving average formula:
// new average = old average * (n-len(M))/n + (sum of values in M)/n)
// where n is the old average data count, and M is the new data set
func (b *Blockchain) calcRollingAverage(newValues []*big.Int, sum *big.Int) {
	var (
		// Save references to old counts
		oldCount   = b.gpAverage.count
		oldAverage = b.gpAverage.price

		inputSetCount = big.NewInt(0).SetInt64(int64(len(newValues)))
	)

	// old average * (n-len(M))/n
	newAverage := big.NewInt(0).Div(
		big.NewInt(0).Mul(
			oldAverage,
			big.NewInt(0).Sub(oldCount, inputSetCount),
		),
		oldCount,
	)

	// + (sum of values in M)/n
	newAverage.Add(
		newAverage,
		big.NewInt(0).Div(
			sum,
			oldCount,
		),
	)

	// Update the references
	b.gpAverage.price = newAverage
	b.gpAverage.count = inputSetCount.Add(inputSetCount, b.gpAverage.count)
}

// calcArithmeticAverage calculates and sets the arithmetic average
// of the passed in data set
func (b *Blockchain) calcArithmeticAverage(newValues []*big.Int, sum *big.Int) {
	newAverageCount := big.NewInt(int64(len(newValues)))
	newAverage := sum.Div(sum, newAverageCount)

	b.gpAverage.price = newAverage
	b.gpAverage.count = newAverageCount
}

// GetHashHelper is used by the EVM, so that the SC can get the hash of the header number
func (b *Blockchain) GetHashHelper(header *types.Header) func(i uint64) (res types.Hash) {
	return func(i uint64) (res types.Hash) {
		num, hash := header.Number-1, header.ParentHash

		for {
			if num == i {
				res = hash

				return
			}

			h, ok := b.GetHeaderByHash(hash)
			if !ok {
				return
			}

			hash = h.ParentHash

			if num == 0 {
				return
			}

			num--
		}
	}
}

type BlockResult struct {
	Root     types.Hash
	Receipts []*types.Receipt
	TotalGas uint64
}

// executeBlockTransactions executes the transactions in the block locally,
// and reports back the block execution result
func (b *Blockchain) executeBlockTransactions(block *types.Block) (*BlockResult, error) {
	if b.isStopped() {
		return nil, ErrClosed
	}

	b.wg.Add(1)
	defer b.wg.Done()

	header := block.Header

	parent, err := rawdb.ReadHeader(b.chaindb, header.ParentHash)
	if err != nil {
		return nil, ErrParentNotFound
	}

	height := header.Number

	blockCreator, err := ecrecoverFromHeader(header)
	if err != nil {
		return nil, err
	}

	// prepare execution
	txn, err := b.executor.BeginTxn(parent.StateRoot, block.Header, blockCreator)
	if err != nil {
		return nil, err
	}

	// upgrade system contract first if needed
	upgrader.UpgradeSystem(
		b.config.Params.ChainID,
		b.config.Params.Forks,
		block.Number(),
		txn.Txn(),
		b.logger,
	)

	// there might be 2 system transactions, slash or deposit
	systemTxs := make([]*types.Transaction, 0, 2)
	// normal transactions which is not consensus associated
	normalTxs := make([]*types.Transaction, 0, len(block.Transactions))

	// the include sequence should be same as execution, otherwise it failed on state root comparison
	for _, tx := range block.Transactions {
		if b.IsSystemTransaction(height, blockCreator, tx) {
			systemTxs = append(systemTxs, tx)
			continue
		}

		normalTxs = append(normalTxs, tx)
	}

	if len(normalTxs) > 0 {
		// execute normal transaction first
		if _, err := b.executor.ProcessTransactions(txn, header.GasLimit, normalTxs); err != nil {
			return nil, err
		}
	}

	if len(systemTxs) > 0 {
		if _, err := b.executor.ProcessTransactions(txn, header.GasLimit, systemTxs); err != nil {
			return nil, err
		}
	}

	if b.isStopped() {
		// execute stop, should not commit
		return nil, ErrClosed
	}

	// commit world state
	_, root := txn.Commit()

	return &BlockResult{
		Root:     root,
		Receipts: txn.Receipts(),
		TotalGas: txn.TotalGas(),
	}, nil
}

func (b *Blockchain) IsSystemTransaction(height uint64, coinbase types.Address, tx *types.Transaction) bool {

	if !b.config.Params.Forks.At(height).Detroit {
		return false
	}

	if b.isDepositTx(height, coinbase, tx) {
		return true
	}

	return b.isSlashTx(height, coinbase, tx)
}

func (b *Blockchain) isSlashTx(height uint64, coinbase types.Address, tx *types.Transaction) bool {
	if tx.To == nil || *tx.To != systemcontracts.AddrValidatorSetContract {
		return false
	}

	// check input
	if !validatorset.IsSlashTransactionSignture(tx.Input) {
		return false
	}

	// signer by height
	signer := b.getSigner(height)

	// tx sender
	from, err := signer.Sender(tx)
	if err != nil {
		return false
	}

	return from == coinbase
}

func (b *Blockchain) isDepositTx(height uint64, coinbase types.Address, tx *types.Transaction) bool {
	if tx.To == nil || *tx.To != systemcontracts.AddrValidatorSetContract {
		return false
	}

	// check input
	if !validatorset.IsDepositTransactionSignture(tx.Input) {
		return false
	}

	// signer by height
	signer := b.getSigner(height)

	// tx sender
	from, err := signer.Sender(tx)
	if err != nil {
		return false
	}

	return from == coinbase
}

func (b *Blockchain) getSigner(height uint64) crypto.TxSigner {
	return crypto.NewSigner(
		b.config.Params.Forks.At(height),
		uint64(b.config.Params.ChainID),
	)
}

// writeBody writes the block body to the DB.
// Additionally, it also updates the txn lookup, for txnHash -> block lookups
func (b *Blockchain) writeBody(block *types.Block) error {
	return rawdb.WriteTransactions(b.chaindb, block.Transactions)
}

func (b *Blockchain) VerifyFinalizedBlock(block *types.Block) error {
	if b.isStopped() {
		return ErrClosed
	}

	b.wg.Add(1)
	defer b.wg.Done()

	if block == nil {
		return ErrNoBlock
	}

	if block.Header == nil {
		return ErrNoBlockHeader
	}

	if latest, ok := rawdb.ReadHeadNumber(b.chaindb); ok {
		if latest > block.Number() {
			return ErrExistBlock
		}
	}

	// Make sure the consensus layer verifies this block header
	if err := b.VerifyHeader(block.Header); err != nil {
		return fmt.Errorf("failed to verify the header: %w", err)
	}

	// Make sure the transactions root matches up
	if hash := buildroot.CalculateTransactionsRoot(block.Transactions); hash != block.Header.TxRoot {
		b.logger.Error(fmt.Sprintf(
			"transaction root hash mismatch: have %s, want %s",
			hash,
			block.Header.TxRoot,
		))

		return ErrInvalidTxRoot
	}

	return nil
}

// CalculateGasLimit returns the gas limit of the next block after parent
func (b *Blockchain) CalculateGasLimit(number uint64) (uint64, error) {
	parent, ok := b.GetHeaderByNumber(number - 1)
	if !ok {
		return 0, fmt.Errorf("parent of block %d not found", number)
	}

	return b.calculateGasLimit(parent.GasLimit), nil
}

const BlockGasTargetDivisor uint64 = 1024 // The bound divisor of the gas limit, used in update calculations

// calculateGasLimit calculates gas limit in reference to the block gas target
func (b *Blockchain) calculateGasLimit(parentGasLimit uint64) uint64 {
	// The gas limit cannot move more than 1/1024 * parentGasLimit
	// in either direction per block
	blockGasTarget := b.config.Params.BlockGasTarget

	// Check if the gas limit target has been set
	if blockGasTarget == 0 {
		// The gas limit target has not been set,
		// so it should use the parent gas limit
		return parentGasLimit
	}

	// Check if the gas limit is already at the target
	if parentGasLimit == blockGasTarget {
		// The gas limit is already at the target, no need to move it
		return blockGasTarget
	}

	delta := parentGasLimit * 1 / BlockGasTargetDivisor
	if parentGasLimit < blockGasTarget {
		// The gas limit is lower than the gas target, so it should
		// increase towards the target
		return common.Min(blockGasTarget, parentGasLimit+delta)
	}

	// The gas limit is higher than the gas target, so it should
	// decrease towards the target
	return common.Max(blockGasTarget, common.Max(parentGasLimit-delta, 0))
}

// initCaches initializes the blockchain caches with the specified size
func (b *Blockchain) initCaches(size int) error {
	var err error

	b.headersCache, err = lru.New(size)
	if err != nil {
		return fmt.Errorf("unable to create headers cache, %w", err)
	}

	b.blockNumberHashCache, err = lru.New(size)
	if err != nil {
		return fmt.Errorf("unable to create canonical cache, %w", err)
	}

	b.difficultyCache, err = lru.New(size)
	if err != nil {
		return fmt.Errorf("unable to create difficulty cache, %w", err)
	}

	return nil
}

func (b *Blockchain) ChainDB() ethdb.Database {
	return b.chaindb
}

func (b *Blockchain) HandleGenesis() error {

	head, ok := rawdb.ReadHeadHash(b.chaindb)
	if ok { // non empty storage
		b.SelfCheck()
		genesis, ok := rawdb.ReadCanonicalHash(b.chaindb, 0)
		if !ok {
			return fmt.Errorf("failed to load genesis hash")
		}
		// check genesis hash
		if genesis != b.config.Genesis.Hash() {
			return fmt.Errorf("genesis file does not match current genesis")
		}

		header, err := rawdb.ReadHeader(b.chaindb, head)
		if err != nil {
			return fmt.Errorf("failed to get header with hash %s err: %v", head.String(), err)
		}
		b.logger.Info("current header", "hash", head.String(), "number", header.Number)

		b.setCurHeader(header, header.Difficulty)

	} else { // empty storage, write the genesis

		if err := b.writeGenesis(b.config.Genesis); err != nil {
			return err
		}
	}

	b.logger.Info("genesis", "hash", b.config.Genesis.Hash())

	return nil
}

func (b *Blockchain) WriteHeader(header *types.Header) error {
	err := rawdb.WriteHeader(b.chaindb, header)
	if err != nil {
		return fmt.Errorf("failed to write header %s %v", header.Hash, err)
	}

	// Advance the head
	if _, err = b.advanceHead(header); err != nil {
		return err
	}

	// Create an event and send it to the stream
	event := &Event{}
	event.AddNewHeader(header)
	b.stream.push(event)
	return nil
}

func (b *Blockchain) writeGenesis(genesis *chain.Genesis) error {

	header := genesis.GenesisHeader()
	header.ComputeHash()
	b.genesis = header.Hash

	if err := rawdb.WriteTD(b.chaindb, header.Hash, 1); err != nil {
		return fmt.Errorf("write td failed %v", err)
	}

	return b.WriteHeader(header)
}

func (b *Blockchain) VerifyHeader(header *types.Header) error {

	if header.Number-1 == 0 {
		return nil
	}

	// check parent hash
	hash, ok := rawdb.ReadCanonicalHash(b.chaindb, header.Number-1)
	if !ok {
		return fmt.Errorf("not found block %d ", header.Number-1)
	}
	parent, err := rawdb.ReadHeader(b.chaindb, hash)
	if err != nil {
		return fmt.Errorf("get parent header %v", err)
	}
	if parent.Hash != header.ParentHash {
		return fmt.Errorf("unexpected header %s != %s %s", parent.Hash, header.ParentHash, parent.ComputeHash().Hash)
	}
	// check header self hash
	if header.Hash != types.HeaderHash(header) {
		return fmt.Errorf("header self check err %s != %s", header.Hash, types.HeaderHash(header))
	}
	return nil
}

func (b *Blockchain) addHeaderSnap(header *types.Header) error {

	extra, err := types.GetIbftExtra(header)
	if err != nil {
		return err
	}
	s := &types.Snapshot{
		Hash:   header.Hash.String(),
		Number: header.Number,
		Votes:  []*types.Vote{},
		Set:    extra.Validators,
	}

	return rawdb.WriteSnap(b.chaindb, header.Number, s)
}

func (b *Blockchain) advanceHead(newHeader *types.Header) (*big.Int, error) {
	err := rawdb.WriteHeadHash(b.chaindb, newHeader.Hash)
	if err != nil {
		return nil, err
	}

	err = rawdb.WriteHeadNumber(b.chaindb, newHeader.Number)
	if err != nil {
		return nil, err
	}

	err = rawdb.WriteCanonicalHash(b.chaindb, newHeader.Number, newHeader.Hash)
	if err != nil {
		return nil, err
	}

	// Calculate the new total difficulty
	if err := rawdb.WriteTD(b.chaindb, newHeader.Hash, newHeader.Difficulty); err != nil {
		return nil, err
	}

	// Update the blockchain reference
	b.setCurHeader(newHeader, newHeader.Difficulty)

	return nil, nil
}

func (b *Blockchain) readTotalDifficulty(headerHash types.Hash) (*big.Int, bool) {
	// Try to find the difficulty in the cache
	foundDifficulty, ok := b.difficultyCache.Get(headerHash)
	if ok {
		// Hit, return the difficulty
		fd, ok := foundDifficulty.(*big.Int)
		if !ok {
			return nil, false
		}

		return fd, true
	}

	// Miss, read the difficulty from the DB
	dbDifficulty, ok := rawdb.ReadTD(b.chaindb, headerHash)
	if !ok {
		return nil, false
	}

	// Update the difficulty cache
	b.difficultyCache.Add(headerHash, dbDifficulty)

	return dbDifficulty, true
}

func (b *Blockchain) setCurHeader(header *types.Header, diff uint64) {
	b.currentHeader.Store(header.Copy())
	b.currentDifficulty.Store(big.NewInt(int64(diff)))
}

func (b *Blockchain) Header() *types.Header {

	header, ok := b.currentHeader.Load().(*types.Header)
	if !ok {
		return nil
	}

	return header
}

func (b *Blockchain) GetBlockByNumber(blockNumber uint64, full bool) (*types.Block, bool) {
	blkHash, ok := rawdb.ReadCanonicalHash(b.chaindb, blockNumber)
	if !ok {
		return nil, false
	}
	return rawdb.ReadBlockByHash(b.chaindb, blkHash)
}

// SubscribeEvents returns a blockchain event subscription
func (b *Blockchain) SubscribeEvents() Subscription {
	return b.stream.subscribe()
}
