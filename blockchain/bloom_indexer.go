package blockchain

import (
	"context"
	"github.com/ankr/dogesyncer/ethdb"
	"github.com/ankr/dogesyncer/helper/bitutil"
	"github.com/ankr/dogesyncer/rawdb"
	"github.com/ankr/dogesyncer/types"
	"github.com/hashicorp/go-hclog"
	"time"
)

const (
	// bloomThrottling is the time to wait between processing two consecutive index
	// sections. It's useful during chain upgrades to prevent disk overload.
	bloomThrottling            = 100 * time.Millisecond
	bloomBitsBlocks     uint64 = 4096
	bloomConfirms              = 256
	bloomFilterThreads         = 3
	bloomRetrievalBatch        = 16
	bloomRetrievalWait         = time.Duration(0)
	bloomServiceThreads        = 16
)

// BloomIndexer implements a core.ChainIndexer, building up a rotated bloom bits index
// for the Ethereum header bloom filters, permitting blazing fast filtering.
type BloomIndexer struct {
	size    uint64           // section size to generate bloombits for
	db      ethdb.Database   // database instance to write index data and metadata into
	gen     *types.Generator // generator to rotate the bloom bits crating the bloom index
	section uint64           // Section is the section number being processed currently
	head    types.Hash       // Head is the hash of the last header processed
}

// NewBloomIndexer returns a chain indexer that generates bloom bits data for the
// canonical chain for fast logs filtering.
func NewBloomIndexer(db ethdb.Database, size, confirms uint64, logger hclog.Logger) *ChainIndexer {
	backend := &BloomIndexer{
		db:   db,
		size: size,
	}

	return NewChainIndexer(db, backend, size, confirms, bloomThrottling, logger)
}

// Reset implements core.ChainIndexerBackend, starting a new bloombits index
// section.
func (b *BloomIndexer) Reset(ctx context.Context, section uint64, lastSectionHead types.Hash) error {
	gen, err := types.NewGenerator(uint(b.size))
	b.gen, b.section, b.head = gen, section, types.Hash{}
	return err
}

// Process implements core.ChainIndexerBackend, adding a new header's bloom into
// the index.
func (b *BloomIndexer) Process(ctx context.Context, header *types.Header) error {
	_ = b.gen.AddBloom(uint(header.Number-b.section*b.size), header.LogsBloom)
	b.head = header.Hash
	return nil
}

// Commit implements core.ChainIndexerBackend, finalizing the bloom section and
// writing it out into the database.
func (b *BloomIndexer) Commit() error {
	batch := b.db.Batch()
	for i := 0; i < types.BloomBitLength; i++ {
		bits, err := b.gen.Bitset(uint(i))
		if err != nil {
			return err
		}
		if err := rawdb.WriteBloomBits(batch, uint(i), b.section, b.head, bitutil.CompressBytes(bits)); err != nil {
			return err
		}
	}
	return batch.Write()
}

// Prune returns an empty error since we don't support pruning here.
func (b *BloomIndexer) Prune(threshold uint64) error {
	return nil
}
