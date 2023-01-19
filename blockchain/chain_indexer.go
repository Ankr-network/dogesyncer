package blockchain

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/ankr/dogesyncer/ethdb"
	"github.com/ankr/dogesyncer/rawdb"
	"github.com/ankr/dogesyncer/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/hashicorp/go-hclog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	bloomIndexerCount = "count"
	bloomSectionHead  = "shead"
)

type ChainHeadEvent struct{ Block *types.Block }

// ChainIndexerBackend defines the methods needed to process chain segments in
// the background and write the segment results into the database. These can be
// used to create filter blooms or CHTs.
type ChainIndexerBackend interface {
	// Reset initiates the processing of a new chain segment, potentially terminating
	// any partially completed operations (in case of a reorg).
	Reset(ctx context.Context, section uint64, prevHead types.Hash) error

	// Process crunches through the next header in the chain segment. The caller
	// will ensure a sequential order of headers.
	Process(ctx context.Context, header *types.Header) error

	// Commit finalizes the section metadata and stores it into the database.
	Commit() error

	// Prune deletes the chain index older than the given threshold.
	Prune(threshold uint64) error
}

// ChainIndexerChain interface is used for connecting the indexer to a blockchain
type ChainIndexerChain interface {
	Header() *types.Header
	SubscribeEvents() Subscription
}

// ChainIndexer does a post-processing job for equally sized sections of the
// canonical chain (like BlooomBits and CHT structures). A ChainIndexer is
// connected to the blockchain through the event system by starting a
// ChainHeadEventLoop in a goroutine.
//
// Further child ChainIndexers can be added which use the output of the parent
// section indexer. These child indexers receive new head notifications only
// after an entire section has been finished or in case of rollbacks that might
// affect already finished sections.
type ChainIndexer struct {
	chainDb  ethdb.Database      // Chain database to index the data from
	backend  ChainIndexerBackend // Background processor generating the index data content
	children []*ChainIndexer     // Child indexers to cascade chain updates to

	active    uint32          // Flag whether the event loop was started
	update    chan struct{}   // Notification channel that headers should be processed
	quit      chan chan error // Quit channel to tear down running goroutines
	ctx       context.Context
	ctxCancel func()

	sectionSize uint64 // Number of blocks in a single chain segment to process
	confirmsReq uint64 // Number of confirmations before processing a completed segment

	storedSections uint64 // Number of sections successfully indexed into the database
	knownSections  uint64 // Number of sections known to be complete (block wise)
	cascadedHead   uint64 // Block number of the last completed section cascaded to subindexers

	checkpointSections uint64     // Number of sections covered by the checkpoint
	checkpointHead     types.Hash // Section head belonging to the checkpoint

	throttling time.Duration // Disk throttling to prevent a heavy upgrade from hogging resources

	log  hclog.Logger
	lock sync.Mutex
}

// NewChainIndexer creates a new chain indexer to do background processing on
// chain segments of a given size after certain number of confirmations passed.
// The throttling parameter might be used to prevent database thrashing.
func NewChainIndexer(chainDb ethdb.Database, backend ChainIndexerBackend, section, confirm uint64, throttling time.Duration, logger hclog.Logger) *ChainIndexer {
	c := &ChainIndexer{
		chainDb:     chainDb,
		backend:     backend,
		update:      make(chan struct{}, 1),
		quit:        make(chan chan error),
		sectionSize: section,
		confirmsReq: confirm,
		throttling:  throttling,
		log:         logger,
	}
	// Initialize database dependent fields and start the updater
	c.loadValidSections()
	c.ctx, c.ctxCancel = context.WithCancel(context.Background())

	go c.updateLoop()

	return c
}

// AddCheckpoint adds a checkpoint. Sections are never processed and the chain
// is not expected to be available before this point. The indexer assumes that
// the backend has sufficient information available to process subsequent sections.
//
// Note: knownSections == 0 and storedSections == checkpointSections until
// syncing reaches the checkpoint
func (c *ChainIndexer) AddCheckpoint(section uint64, shead types.Hash) {
	c.lock.Lock()
	defer c.lock.Unlock()

	// Short circuit if the given checkpoint is below than local's.
	if c.checkpointSections >= section+1 || section < c.storedSections {
		return
	}
	c.checkpointSections = section + 1
	c.checkpointHead = shead

	c.setSectionHead(section, shead)
	c.setValidSections(section + 1)
}

// Start creates a goroutine to feed chain head events into the indexer for
// cascading background processing. Children do not need to be started, they
// are notified about new events by their parents.
func (c *ChainIndexer) Start(chain ChainIndexerChain) {
	sub := chain.SubscribeEvents()

	go c.eventLoop(chain.Header(), sub)
}

// Close tears down all goroutines belonging to the indexer and returns any error
// that might have occurred internally.
func (c *ChainIndexer) Close() error {
	var errs []error

	c.ctxCancel()

	// Tear down the primary update loop
	errc := make(chan error)
	c.quit <- errc
	if err := <-errc; err != nil {
		errs = append(errs, err)
	}
	// If needed, tear down the secondary event loop
	if atomic.LoadUint32(&c.active) != 0 {
		c.quit <- errc
		if err := <-errc; err != nil {
			errs = append(errs, err)
		}
	}
	// Close all children
	for _, child := range c.children {
		if err := child.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	// Return any failures
	switch {
	case len(errs) == 0:
		return nil

	case len(errs) == 1:
		return errs[0]

	default:
		return fmt.Errorf("%v", errs)
	}
}

// eventLoop is a secondary - optional - event loop of the indexer which is only
// started for the outermost indexer to push chain head events into a processing
// queue.
func (c *ChainIndexer) eventLoop(currentHeader *types.Header, sub Subscription) {
	// Mark the chain indexer as active, requiring an additional teardown
	atomic.StoreUint32(&c.active, 1)

	defer sub.Close()

	events := sub.GetEventCh()

	// Fire the initial new head event to start any outstanding processing
	c.newHead(currentHeader.Number, false)

	var (
		prevHeader = currentHeader
		prevHash   = currentHeader.Hash
	)
	for {
		select {
		case errc := <-c.quit:
			// Chain indexer terminating, report no failure and abort
			errc <- nil
			return

		case ev, ok := <-events:
			// Received a new event, ensure it's not nil (closing) and update
			if !ok {
				errc := <-c.quit
				errc <- nil
				return
			}
			header := ev.Header()
			if header.ParentHash != prevHash {
				// Reorg to the common ancestor if needed (might not exist in light sync mode, skip reorg then)
				// TODO(karalabe, zsfelfoldi): This seems a bit brittle, can we detect this case explicitly?

				if hash, _ := rawdb.ReadCanonicalHash(c.chainDb, prevHeader.Number); hash != prevHash {
					c.log.Warn("can not found previous header!!!", "previous block should be hash: ", prevHash.String(), " but get: ", hash.String())
					/*	todo if h := rawdb.FindCommonAncestor(c.chainDb, prevHeader, header); h != nil {
						c.newHead(h.Number, true)
					}*/
				}
			}
			c.newHead(header.Number, false)

			prevHeader, prevHash = header, header.Hash
		}
	}
}

// newHead notifies the indexer about new chain heads and/or reorgs.
func (c *ChainIndexer) newHead(head uint64, reorg bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	// If a reorg happened, invalidate all sections until that point
	if reorg {
		// Revert the known section number to the reorg point
		known := (head + 1) / c.sectionSize
		stored := known
		if known < c.checkpointSections {
			known = 0
		}
		if stored < c.checkpointSections {
			stored = c.checkpointSections
		}
		if known < c.knownSections {
			c.knownSections = known
		}
		// Revert the stored sections from the database to the reorg point
		if stored < c.storedSections {
			c.setValidSections(stored)
		}
		// Update the new head number to the finalized section end and notify children
		head = known * c.sectionSize

		if head < c.cascadedHead {
			c.cascadedHead = head
			for _, child := range c.children {
				child.newHead(c.cascadedHead, true)
			}
		}
		return
	}
	// No reorg, calculate the number of newly known sections and update if high enough
	var sections uint64
	if head >= c.confirmsReq {
		sections = (head + 1 - c.confirmsReq) / c.sectionSize
		if sections < c.checkpointSections {
			sections = 0
		}
		if sections > c.knownSections {
			if c.knownSections < c.checkpointSections {
				// syncing reached the checkpoint, verify section head
				syncedHead, _ := rawdb.ReadCanonicalHash(c.chainDb, c.checkpointSections*c.sectionSize-1)
				if syncedHead != c.checkpointHead {
					c.log.Error("Synced chain does not match checkpoint", "number", c.checkpointSections*c.sectionSize-1, "expected", c.checkpointHead, "synced", syncedHead)
					return
				}
			}
			c.knownSections = sections

			select {
			case c.update <- struct{}{}:
			default:
			}
		}
	}
}

// updateLoop is the main event loop of the indexer which pushes chain segments
// down into the processing backend.
func (c *ChainIndexer) updateLoop() {
	var (
		updating bool
		updated  time.Time
	)

	for {
		select {
		case errc := <-c.quit:
			// Chain indexer terminating, report no failure and abort
			errc <- nil
			return

		case <-c.update:
			// Section headers completed (or rolled back), update the index
			c.lock.Lock()
			if c.knownSections > c.storedSections {
				// Periodically print an upgrade log message to the user
				if time.Since(updated) > 8*time.Second {
					if c.knownSections > c.storedSections+1 {
						updating = true
						c.log.Info("Upgrading chain index", "percentage", c.storedSections*100/c.knownSections)
					}
					updated = time.Now()
				}
				// Cache the current section count and head to allow unlocking the mutex
				c.verifyLastHead()
				section := c.storedSections
				var oldHead types.Hash
				if section > 0 {
					oldHead = c.SectionHead(section - 1)
				}
				// Process the newly defined section in the background
				c.lock.Unlock()
				newHead, err := c.processSection(section, oldHead)
				if err != nil {
					select {
					case <-c.ctx.Done():
						<-c.quit <- nil
						return
					default:
					}
					c.log.Error("Section processing failed", "error", err)
				}
				c.lock.Lock()

				// If processing succeeded and no reorgs occurred, mark the section completed
				if err == nil && (section == 0 || oldHead == c.SectionHead(section-1)) {
					c.setSectionHead(section, newHead)
					c.setValidSections(section + 1)
					if c.storedSections == c.knownSections && updating {
						updating = false
						c.log.Info("Finished upgrading chain index")
					}
					c.cascadedHead = c.storedSections*c.sectionSize - 1
					for _, child := range c.children {
						c.log.Trace("Cascading chain index update", "head", c.cascadedHead)
						child.newHead(c.cascadedHead, false)
					}
				} else {
					// If processing failed, don't retry until further notification
					c.log.Debug("Chain index processing failed", "section", section, "err", err)
					c.verifyLastHead()
					c.knownSections = c.storedSections
				}
			}
			// If there are still further sections to process, reschedule
			if c.knownSections > c.storedSections {
				time.AfterFunc(c.throttling, func() {
					select {
					case c.update <- struct{}{}:
					default:
					}
				})
			}
			c.lock.Unlock()
		}
	}
}

// processSection processes an entire section by calling backend functions while
// ensuring the continuity of the passed headers. Since the chain mutex is not
// held while processing, the continuity can be broken by a long reorg, in which
// case the function returns with an error.
func (c *ChainIndexer) processSection(section uint64, lastHead types.Hash) (types.Hash, error) {
	c.log.Trace("Processing new chain section", "section", section)

	// Reset and partial processing
	if err := c.backend.Reset(c.ctx, section, lastHead); err != nil {
		c.setValidSections(0)
		return types.Hash{}, err
	}

	for number := section * c.sectionSize; number < (section+1)*c.sectionSize; number++ {
		hash, _ := rawdb.ReadCanonicalHash(c.chainDb, number)
		if hash == (types.Hash{}) {
			return types.Hash{}, fmt.Errorf("canonical block #%d unknown", number)
		}
		header, _ := rawdb.ReadHeader(c.chainDb, hash)
		if header == nil {
			return types.Hash{}, fmt.Errorf("block #%d [%x..] not found", number, hash[:4])
		} else if header.ParentHash != lastHead {
			return types.Hash{}, fmt.Errorf("chain reorged during section %d  processing. want: %s have: %s", section, lastHead, header.ParentHash)
		}
		if err := c.backend.Process(c.ctx, header); err != nil {
			return types.Hash{}, err
		}
		lastHead = header.Hash
	}
	if err := c.backend.Commit(); err != nil {
		return types.Hash{}, err
	}
	return lastHead, nil
}

// verifyLastHead compares last stored section head with the corresponding block hash in the
// actual canonical chain and rolls back reorged sections if necessary to ensure that stored
// sections are all valid
func (c *ChainIndexer) verifyLastHead() {
	for c.storedSections > 0 && c.storedSections > c.checkpointSections {
		hash, _ := rawdb.ReadCanonicalHash(c.chainDb, c.storedSections*c.sectionSize-1)
		if c.SectionHead(c.storedSections-1) == hash {
			return
		}
		c.setValidSections(c.storedSections - 1)
	}
}

// Sections returns the number of processed sections maintained by the indexer
// and also the information about the last header indexed for potential canonical
// verifications.
func (c *ChainIndexer) Sections() (uint64, uint64, types.Hash) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.verifyLastHead()
	return c.storedSections, c.storedSections*c.sectionSize - 1, c.SectionHead(c.storedSections - 1)
}

// AddChildIndexer adds a child ChainIndexer that can use the output of this one
func (c *ChainIndexer) AddChildIndexer(indexer *ChainIndexer) {
	if indexer == c {
		panic("can't add indexer as a child of itself")
	}
	c.lock.Lock()
	defer c.lock.Unlock()

	c.children = append(c.children, indexer)

	// Cascade any pending updates to new children too
	sections := c.storedSections
	if c.knownSections < sections {
		// if a section is "stored" but not "known" then it is a checkpoint without
		// available chain data so we should not cascade it yet
		sections = c.knownSections
	}
	if sections > 0 {
		indexer.newHead(sections*c.sectionSize-1, false)
	}
}

// Prune deletes all chain data older than given threshold.
func (c *ChainIndexer) Prune(threshold uint64) error {
	return c.backend.Prune(threshold)
}

// loadValidSections reads the number of valid sections from the index database
// and caches is into the local state.
func (c *ChainIndexer) loadValidSections() {
	data, _, _ := c.chainDb.Get(ethdb.BloomBitsIndexPrefix, []byte(bloomIndexerCount))
	if len(data) == 8 {
		c.storedSections = binary.BigEndian.Uint64(data)
	}

	fmt.Println("------DEBUG----ChainIndexer----------------", c.storedSections)
}

// setValidSections writes the number of valid sections to the index database
func (c *ChainIndexer) setValidSections(sections uint64) {
	// Set the current number of valid sections in the database
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], sections)
	c.chainDb.Set(ethdb.BloomBitsIndexPrefix, []byte(bloomIndexerCount), data[:])

	// Remove any reorged sections, caching the valid in the meantime
	for c.storedSections > sections {
		c.storedSections--
		c.removeSectionHead(c.storedSections)
	}
	c.storedSections = sections // needed if new > old
}

// SectionHead retrieves the last block hash of a processed section from the
// index database.
func (c *ChainIndexer) SectionHead(section uint64) types.Hash {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], section)

	hash, _, _ := c.chainDb.Get(ethdb.BloomBitsIndexPrefix, append([]byte(bloomSectionHead), data[:]...))
	if len(hash) == len(common.Hash{}) {
		return types.BytesToHash(hash)
	}
	return types.Hash{}
}

// setSectionHead writes the last block hash of a processed section to the index
// database.
func (c *ChainIndexer) setSectionHead(section uint64, hash types.Hash) {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], section)

	_ = c.chainDb.Set(ethdb.BloomBitsIndexPrefix, append([]byte(bloomSectionHead), data[:]...), hash.Bytes())
}

// removeSectionHead removes the reference to a processed section from the index
// database.
func (c *ChainIndexer) removeSectionHead(section uint64) {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], section)

	_ = c.chainDb.Remove(ethdb.BloomBitsIndexPrefix, append([]byte(bloomSectionHead), data[:]...))
}