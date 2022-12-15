package rpc

import (
	"math/big"

	"github.com/sunvim/dogesyncer/helper/hex"
	"github.com/sunvim/dogesyncer/helper/progress"
	"github.com/sunvim/dogesyncer/state"
	"github.com/sunvim/dogesyncer/state/runtime"
	"github.com/sunvim/dogesyncer/types"
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
	// priceLimit uint64
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

func (s *RpcServer) EthSyncing(method string, params ...any) (any, Error) {
	res, err := s.endpoints.Eth.Syncing()
	if err != nil {
		return nil, NewInternalError(err.Error())
	}
	return res, nil
}
