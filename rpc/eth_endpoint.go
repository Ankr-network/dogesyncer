package rpc

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/sunvim/dogesyncer/helper/hex"
	"github.com/sunvim/dogesyncer/helper/progress"
	"github.com/sunvim/dogesyncer/state"
	"github.com/sunvim/dogesyncer/state/runtime"
	"github.com/sunvim/dogesyncer/types"
)

const (
	defaultMinGasPrice = "0Xba43b7400" // 50 GWei
)

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
	priceLimit uint64
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
	priceLimit := new(big.Int).SetUint64(e.priceLimit)
	minGasPrice, _ := new(big.Int).SetString(defaultMinGasPrice, 0)

	if priceLimit.Cmp(minGasPrice) == -1 {
		priceLimit = minGasPrice
	}

	// if e.store.GetAvgGasPrice().Cmp(minGasPrice) == -1 {
	// 	avgGasPrice = hex.EncodeBig(minGasPrice)
	// } else {
	// 	avgGasPrice = hex.EncodeBig(e.store.GetAvgGasPrice())
	// }

	// return avgGasPrice, nil

	return hex.EncodeBig(priceLimit), nil
}

// GetBlockByHash returns information about a block by hash
func (e *Eth) GetBlockByHash(hash types.Hash, fullTx bool) (interface{}, error) {
	block, ok := e.store.GetBlockByHash(hash, true)
	if !ok {
		return nil, nil
	}

	return toBlock(block, fullTx), nil
}

// TODO
func (e *Eth) GetTransactionByHash(hash types.Hash) (interface{}, error) {
	_, ok := e.store.GetTxnByHash(hash)
	if !ok {
		return nil, nil
	}
	return nil, nil
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
	num := strconv.FormatInt(int64(s.blockchain.Header().Number), 16)
	return strings.Join([]string{"0x", num}, ""), nil
}

func (s *RpcServer) EthGetBlockByHash(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, err
	}
	res, errWbe3 := s.endpoints.Eth.GetBlockByHash(types.StringToHash(paramsIn[0]), true)
	if errWbe3 != nil {
		return nil, NewInvalidRequestError(errWbe3.Error())
	}
	return res, nil
}
