package pebble

import (
	"log"

	"github.com/ankr/dogesyncer/ethdb"
	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
	"github.com/hashicorp/go-hclog"
)

type pebbleDB struct {
	logger hclog.Logger
	d      *pebble.DB
	wo     *pebble.WriteOptions
}

func NewPebbleDB(dir string, logger hclog.Logger) ethdb.Database {
	cache := pebble.NewCache(1 << 30)
	defer cache.Unref()
	opts := &pebble.Options{
		Cache:                       cache,
		BytesPerSync:                32 << 20,
		DisableWAL:                  false,
		FormatMajorVersion:          pebble.FormatNewest,
		L0CompactionThreshold:       2,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20, // 64 MB
		Levels:                      make([]pebble.LevelOptions, 7),
		MaxOpenFiles:                16384,
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
		Merger: &pebble.Merger{
			Name: "dogesyncer_merge_operator",
		},
		MaxConcurrentCompactions: func() int {
			return 3
		},
	}

	for i := 0; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		if i > 0 {
			l.TargetFileSize = opts.Levels[i-1].TargetFileSize * 2
			l.Compression = pebble.ZstdCompression
		}
		l.EnsureDefaults()
	}
	opts.Levels[6].FilterPolicy = nil
	opts.FlushSplitBytes = opts.Levels[0].TargetFileSize

	opts.EnsureDefaults()

	p, err := pebble.Open(dir, opts)
	if err != nil {
		log.Fatal(err)
	}
	return &pebbleDB{
		d:      p,
		wo:     pebble.Sync,
		logger: logger,
	}
}
