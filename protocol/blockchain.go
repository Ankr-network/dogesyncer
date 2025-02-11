package protocol

import (
	"math/big"

	"github.com/ankr/dogesyncer/blockchain"
	"github.com/ankr/dogesyncer/ethdb"
	"github.com/ankr/dogesyncer/types"
)

// Blockchain is the interface required by the syncer to connect to the blockchain
type blockchainShim interface {
	SubscribeEvents() blockchain.Subscription
	Header() *types.Header
	CurrentTD() *big.Int

	GetTD(hash types.Hash) (*big.Int, bool)
	GetReceiptsByHash(types.Hash) ([]*types.Receipt, error)
	GetBodyByHash(types.Hash) (*types.Body, bool)
	GetHeaderByHash(types.Hash) (*types.Header, bool)
	GetHeaderByNumber(n uint64) (*types.Header, bool)
	ChainDB() ethdb.Database

	// advance chain methods
	WriteBlock(block *types.Block) error
	VerifyFinalizedBlock(block *types.Block) error
	VerifyHeader(header *types.Header) error
	WriteHeader(header *types.Header) error
	CalculateGasLimit(number uint64) (uint64, error)
}
