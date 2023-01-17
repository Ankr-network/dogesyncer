package rpc

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ankr/dogesyncer/blockchain"
	"gopkg.in/square/go-jose.v2/json"

	"github.com/ankr/dogesyncer/helper/hex"
	"github.com/ankr/dogesyncer/helper/progress"
	"github.com/ankr/dogesyncer/state"
	"github.com/ankr/dogesyncer/state/runtime"
	"github.com/ankr/dogesyncer/types"
)

const (
	defaultMinGasPrice = "0Xba43b7400" // 50 GWei
)

var minGasPrice, _ = new(big.Int).SetString(defaultMinGasPrice, 0)

type ethBlockchainStore interface {
	// Header returns the current header of the chain (genesis if empty)
	Header() *types.Header

	// GetHeaderByNumber returns the header by number
	GetHeaderByNumber(block uint64) (*types.Header, bool)

	// GetBlockByHash gets a block using the provided hash
	GetBlockByHash(hash types.Hash, full bool) (*types.Block, bool)

	// GetBlockByNumber returns a block using the provided number
	GetBlockByNumber(num uint64, full bool) (*types.Block, bool)

	// ReadTxLookup returns a block hash in which a given txn was mined
	ReadTxLookup(txnHash types.Hash) (types.Hash, bool)

	// GetReceiptsByHash returns the receipts for a block hash
	GetReceiptsByHash(hash types.Hash) ([]*types.Receipt, error)

	GetTxnByHash(hash types.Hash) (*types.Transaction, bool)

	// GetAvgGasPrice returns the average gas price
	GetAvgGasPrice() *big.Int

	// ApplyTxn applies a transaction object to the blockchain
	ApplyTxn(header *types.Header, txn *types.Transaction) (*runtime.ExecutionResult, error)

	// GetSyncProgression retrieves the current sync progression, if any
	GetSyncProgression() *progress.Progression

	// StateAtTransaction returns the execution environment of a certain transaction.
	// The transition should not commit, it shall be collected by GC.
	StateAtTransaction(block *types.Block, txIndex int) (*state.Transition, error)
}

// ethStore provides access to the methods needed by eth endpoint
type ethStore interface {
	// ethTxPoolStore
	// ethStateStore
	ethBlockchainStore
}

type Eth struct {
	// logger  hclog.Logger
	store ethStore
	// chainID uint64
	// filterManager *FilterManager
	priceLimit *big.Int
}

func (e *Eth) Syncing() (interface{}, error) {
	if syncProgression := e.store.GetSyncProgression(); syncProgression != nil {
		// Node is bulk syncing, return the status
		return progression{
			Type:          string(syncProgression.SyncType),
			SyncingPeer:   syncProgression.SyncingPeer,
			StartingBlock: hex.EncodeUint64(syncProgression.StartingBlock),
			CurrentBlock:  hex.EncodeUint64(syncProgression.CurrentBlock),
			HighestBlock:  hex.EncodeUint64(syncProgression.HighestBlock),
		}, nil
	}

	// Node is not bulk syncing
	return false, nil
}

// GasPrice returns the average gas price based on the last x blocks
func (e *Eth) GasPrice() (interface{}, error) {
	// var avgGasPrice string
	// Grab the average gas price and convert it to a hex value
	return hex.EncodeBig(e.priceLimit), nil
}

func GetNumericBlockNumber(numberParam string, blockchain *blockchain.Blockchain) (uint64, error) {
	switch numberParam {
	case "latest":
		return blockchain.Header().Number, nil

	case "earliest":
		return 1, nil

	case "pending":
		return 0, fmt.Errorf("fetching the pending header is not supported")

	default:
		return strconv.ParseUint(numberParam, 0, 64)
	}
}

func (s *RpcServer) EthSyncing(method string, params ...any) (any, Error) {
	res, err := s.endpoints.Eth.Syncing()
	if err != nil {
		return nil, NewInternalError(err.Error())
	}
	return res, nil
}

func (s *RpcServer) EthGasPrice(method string, params ...any) (any, Error) {
	res, err := s.endpoints.Eth.GasPrice()
	if err != nil {
		return nil, NewInternalError(err.Error())
	}
	return res, nil
}

func (s *RpcServer) GetBlockNumber(method string, params ...any) (any, Error) {
	return fmt.Sprintf("0x%0x", s.blockchain.Header().Number), nil
}

func (s *RpcServer) EthGetBlockByHash(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if len(paramsIn) != 2 {
		return nil, NewInvalidParamsError(fmt.Sprintf("missing value for required argument %d", len(paramsIn)))
	}
	res, ok := s.blockchain.GetBlockByHash(types.StringToHash(paramsIn[0].(string)), paramsIn[1].(bool))
	if !ok {
		return nil, nil
	}
	return toBlock(res, paramsIn[1].(bool), s.GetTxSigner(res.Number())), nil
}

func (s *RpcServer) EthGetBlockByNumber(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if len(paramsIn) != 2 {
		return nil, NewInvalidParamsError(fmt.Sprintf("missing value for required argument %d", len(paramsIn)))
	}
	blockHeight, strconvErr := strconv.ParseUint(strings.TrimPrefix(paramsIn[0].(string), "0x"), 16, 64)
	if strconvErr != nil {
		return nil, NewInvalidRequestError(strconvErr.Error())
	}
	res, ok := s.blockchain.GetBlockByNumber(blockHeight, paramsIn[1].(bool))
	if !ok {
		return nil, nil
	}
	return toBlock(res, paramsIn[1].(bool), s.GetTxSigner(res.Number())), nil
}

func (s *RpcServer) EthGetTransactionByHash(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if len(paramsIn) != 1 {
		return nil, NewInvalidParamsError(fmt.Sprintf("missing value for required argument %d", len(paramsIn)))
	}
	// tx
	tx, ok := s.blockchain.GetTxnByHash(types.StringToHash(paramsIn[0].(string)))
	if !ok {
		return nil, nil
	}
	// block
	blockHash, ok := s.blockchain.ReadTxLookup(tx.Hash())
	if !ok {
		return nil, nil
	}
	block, ok := s.blockchain.GetBlockByHash(blockHash, true)
	if !ok {
		return nil, nil
	}
	for idx, txn := range block.Transactions {
		if txn.Hash() == tx.Hash() {
			return toTransaction(
				tx,
				argUintPtr(block.Number()),
				argHashPtr(block.Hash()),
				&idx,
				s.GetTxSigner(block.Number()),
			), nil
		}
	}
	return nil, nil
}

func (s *RpcServer) EthGetTransactionByBlockNumberAndIndex(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if len(paramsIn) != 2 {
		return nil, NewInvalidParamsError(fmt.Sprintf("missing value for required argument %d", len(paramsIn)))
	}
	blockNum, numErr := GetNumericBlockNumber(paramsIn[0].(string), s.blockchain)
	if numErr != nil {
		return nil, NewInvalidParamsError(numErr.Error())
	}
	// get block
	block, ok := s.blockchain.GetBlockByNumber(blockNum, true)
	if !ok {
		return nil, nil
	}
	// tx index
	index, indexErr := strconv.ParseUint(strings.TrimPrefix(paramsIn[1].(string), "0x"), 16, 64)
	if indexErr != nil {
		return nil, NewInvalidParamsError(numErr.Error())
	}
	if index >= uint64(len(block.Transactions)) {
		return nil, NewMethodNotFoundError(fmt.Errorf("this transaction is not found").Error())
	}
	tx := block.Transactions[index]
	idx := int(index)
	return toTransaction(
		tx,
		argUintPtr(block.Number()),
		argHashPtr(block.Hash()),
		&idx,
		s.GetTxSigner(block.Number()),
	), nil

}

func (s *RpcServer) EthGetTransactionReceipt(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	// block
	blockHash, ok := s.blockchain.ReadTxLookup(types.StringToHash(paramsIn[0].(string)))
	if !ok {
		return nil, nil
	}
	block, ok := s.blockchain.GetBlockByHash(blockHash, true)
	if !ok {
		s.logger.Warn(
			fmt.Sprintf("Block with hash [%s] not found", blockHash.String()),
		)
		return nil, nil
	}
	receipts, GetReceiptsByHashErr := s.blockchain.GetReceiptsByHash(blockHash)
	if GetReceiptsByHashErr != nil {
		s.logger.Warn(
			fmt.Sprintf("Receipts for block with hash [%s] not found", blockHash.String()),
		)
		return nil, nil
	}
	if len(receipts) == 0 {
		// Receipts not written yet on the db
		s.logger.Warn(
			fmt.Sprintf("No receipts found for block with hash [%s]", blockHash.String()),
		)
		return nil, nil
	}

	// find the transaction in the body
	txIndex := -1

	for i, txn := range block.Transactions {
		if txn.Hash() == types.StringToHash(paramsIn[0].(string)) {
			txIndex = i
			break
		}
	}

	if txIndex == -1 {
		// txn not found
		return nil, nil
	}

	txn := block.Transactions[txIndex]
	raw := receipts[txIndex]

	var errSender error
	if txn.From == emptyFrom {
		// Decrypt the from address
		txn.From, errSender = s.GetTxSigner(block.Number()).Sender(txn)
		if errSender != nil {
			return nil, NewInternalError(state.NewTransitionApplicationError(err, false).Error())
		}
	}

	logs := make([]*Log, len(raw.Logs))
	for indx, elem := range raw.Logs {
		logs[indx] = &Log{
			Address:     elem.Address,
			Topics:      elem.Topics,
			Data:        argBytes(elem.Data),
			BlockHash:   block.Hash(),
			BlockNumber: argUint64(block.Number()),
			TxHash:      txn.Hash(),
			TxIndex:     argUint64(txIndex),
			LogIndex:    argUint64(indx),
			Removed:     false,
		}
	}

	res := &receipt{
		Root:              raw.Root,
		CumulativeGasUsed: argUint64(raw.CumulativeGasUsed),
		LogsBloom:         raw.LogsBloom,
		Status:            argUint64(*raw.Status),
		TxHash:            txn.Hash(),
		TxIndex:           argUint64(txIndex),
		BlockHash:         block.Hash(),
		BlockNumber:       argUint64(block.Number()),
		GasUsed:           argUint64(raw.GasUsed),
		ContractAddress:   raw.ContractAddress,
		FromAddr:          txn.From,
		ToAddr:            txn.To,
		Logs:              logs,
	}

	return res, nil

}

type LogQueryRequest struct {
	BlockHash string `json:"blockhash"`
	FromBlock string `json:"fromBlock"`
	ToBlock   string `json:"toBlock"`

	Addresses []string      `json:"addresses"`
	Topics    []interface{} `json:"topics"`
}

func (s *RpcServer) GetLogs(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	if len(paramsIn) == 0 {
		return nil, NewInvalidParamsError("not enough params")
	}

	query := new(LogQueryRequest)

	d, e := json.Marshal(paramsIn[0])
	if e != nil {
		return nil, NewInvalidParamsError(e.Error())
	}

	if e := json.Unmarshal(d, query); e != nil {
		return nil, NewInvalidParamsError(e.Error())
	}

	fromBlock, err := StringToBlockNumber(query.FromBlock)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	toBlock, err := StringToBlockNumber(query.ToBlock)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	addresses := make([]types.Address, 0, len(query.Addresses))
	for _, address := range query.Addresses {
		addresses = append(addresses, types.StringToAddress(address))
	}
	topics := make([][]types.Hash, 0, len(query.Topics))
	for _, ts := range query.Topics {
		switch ts.(type) {
		case string:
			topics = append(topics, []types.Hash{types.StringToHash(ts.(string))})

		case []interface{}:
			topic := make([]types.Hash, 0, len(ts.([]interface{})))
			for _, t := range ts.([]interface{}) {
				tps, ok := t.(string)
				if !ok {
					return nil, NewInvalidParamsError("invalid topic")
				}
				topic = append(topic, types.StringToHash(tps))
			}
		}
	}

	queryLog := &LogQuery{
		FromBlock: fromBlock,
		ToBlock:   toBlock,
		Addresses: addresses,
		Topics:    topics,
	}

	if query.BlockHash != "" {
		blockHash := types.StringToHash(query.BlockHash)
		queryLog.BlockHash = &blockHash
	}

	logs, e := s.filterManager.GetLogs(queryLog)
	if e != nil {
		return nil, &internalError{err: e.Error()}
	}
	return logs, nil
}

func (s *RpcServer) GetBloomLogs(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	if len(paramsIn) == 0 {
		return nil, NewInvalidParamsError("not enough params")
	}

	query := new(LogQueryRequest)

	d, e := json.Marshal(paramsIn[0])
	if e != nil {
		return nil, NewInvalidParamsError(e.Error())
	}

	if e := json.Unmarshal(d, query); e != nil {
		return nil, NewInvalidParamsError(e.Error())
	}

	fromBlock, err := StringToBlockNumber(query.FromBlock)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	toBlock, err := StringToBlockNumber(query.ToBlock)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	addresses := make([]types.Address, 0, len(query.Addresses))
	for _, address := range query.Addresses {
		addresses = append(addresses, types.StringToAddress(address))
	}
	topics := make([][]types.Hash, 0, len(query.Topics))
	for _, ts := range query.Topics {
		switch ts.(type) {
		case string:
			topics = append(topics, []types.Hash{types.StringToHash(ts.(string))})

		case []interface{}:
			topic := make([]types.Hash, 0, len(ts.([]interface{})))
			for _, t := range ts.([]interface{}) {
				tps, ok := t.(string)
				if !ok {
					return nil, NewInvalidParamsError("invalid topic")
				}
				topic = append(topic, types.StringToHash(tps))
			}
		}
	}

	queryLog := &LogQuery{
		FromBlock: fromBlock,
		ToBlock:   toBlock,
		Addresses: addresses,
		Topics:    topics,
	}

	if query.BlockHash != "" {
		blockHash := types.StringToHash(query.BlockHash)
		queryLog.BlockHash = &blockHash
	}

	logs, e := s.filterManager.GetBloomLogs(queryLog)
	if e != nil {
		return nil, &internalError{err: e.Error()}
	}
	return logs, nil
}

func (s *RpcServer) GetFilterLogs(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	logFilter, e := s.filterManager.GetLogFilterFromID(paramsIn[0].(string))
	if e != nil {
		return nil, &internalError{err: e.Error()}
	}
	logs, e := s.filterManager.GetLogs(logFilter.query)
	if e != nil {
		return nil, &internalError{err: e.Error()}
	}
	return logs, nil
}

func (s *RpcServer) UninstallFilter(method string, params ...any) (any, Error) {
	return s.filterManager.Uninstall(params[0].(string)), nil
}

func (s *RpcServer) NewFilter(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	if len(paramsIn) == 0 {
		return nil, NewInvalidParamsError("not enough params")
	}

	query := new(LogQueryRequest)

	d, e := json.Marshal(paramsIn[0])
	if e != nil {
		return nil, NewInvalidParamsError(e.Error())
	}

	if e := json.Unmarshal(d, query); e != nil {
		return nil, NewInvalidParamsError(e.Error())
	}

	fromBlock, err := StringToBlockNumber(query.FromBlock)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	toBlock, err := StringToBlockNumber(query.ToBlock)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	addresses := make([]types.Address, 0, len(query.Addresses))
	for _, address := range query.Addresses {
		addresses = append(addresses, types.StringToAddress(address))
	}
	topics := make([][]types.Hash, 0, len(query.Topics))
	for _, ts := range query.Topics {
		switch ts.(type) {
		case string:
			topics = append(topics, []types.Hash{types.StringToHash(ts.(string))})

		case []interface{}:
			topic := make([]types.Hash, 0, len(ts.([]interface{})))
			for _, t := range ts.([]interface{}) {
				tps, ok := t.(string)
				if !ok {
					return nil, NewInvalidParamsError("invalid topic")
				}
				topic = append(topic, types.StringToHash(tps))
			}
		}
	}

	queryLog := &LogQuery{
		FromBlock: fromBlock,
		ToBlock:   toBlock,
		Addresses: addresses,
		Topics:    topics,
	}

	if query.BlockHash != "" {
		blockHash := types.StringToHash(query.BlockHash)
		queryLog.BlockHash = &blockHash
	}
	return s.filterManager.NewLogFilter(queryLog, nil), nil
}
