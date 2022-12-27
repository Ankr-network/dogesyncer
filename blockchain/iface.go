package blockchain

import (
	"github.com/ankr/dogesyncer/ethdb"
	"github.com/ankr/dogesyncer/types"
)

type IBlockchain interface {
	Header() *types.Header
	SubscribeEvents() Subscription
	GetBlockByNumber(blockNumber uint64, full bool) (*types.Block, bool)
	ChainDB() ethdb.Database
}
