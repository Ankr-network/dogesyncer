package mdbx

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-hclog"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/sunvim/dogesyncer/helper"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

type NewValue struct {
	Dbi string
	Key []byte
	Val []byte
}

type MdbxDB struct {
	logger hclog.Logger
	mu     sync.Mutex
	path   string
	env    *mdbx.Env
	cache  *lru.Cache[string, *NewValue]
	acache *MemDB
	bcache *MemDB
	syncCh chan struct{}
	dbi    map[string]mdbx.DBI
	stopCh chan struct{}
}

var (
	nvpool = sync.Pool{
		New: func() any {
			return &NewValue{}
		},
	}

	strbuf = sync.Pool{
		New: func() any {
			return bytes.NewBuffer([]byte{})
		},
	}

	defaultFlags = mdbx.Durable | mdbx.NoReadahead | mdbx.Coalesce

	dbis = []string{
		ethdb.BodyDBI,
		ethdb.AssistDBI,
		ethdb.TrieDBI,
		ethdb.NumHashDBI,
		ethdb.TxesDBI,
		ethdb.HeadDBI,
		ethdb.TODBI,
		ethdb.ReceiptsDBI,
		ethdb.SnapDBI,
		ethdb.CodeDBI,
		ethdb.TxLookUpDBI,
	}
)

const (
	cacheSize = 10240
)

func NewMDBX(path string, logger hclog.Logger) *MdbxDB {

	env, err := mdbx.NewEnv()
	if err != nil {
		panic(err)
	}

	if err := env.SetOption(mdbx.OptMaxDB, 32); err != nil {
		panic(err)
	}

	if err := env.SetOption(mdbx.OptRpAugmentLimit, 0x7fffFFFF); err != nil {
		panic(err)
	}

	if err := env.SetOption(mdbx.OptMaxReaders, 32000); err != nil {
		panic(err)
	}

	if err = env.SetOption(mdbx.OptMergeThreshold16dot16Percent, 32768); err != nil {
		panic(err)
	}

	txnDpInitial, err := env.GetOption(mdbx.OptTxnDpInitial)
	if err != nil {
		panic(err)
	}
	if err = env.SetOption(mdbx.OptTxnDpInitial, txnDpInitial*2); err != nil {
		panic(err)
	}

	dpReserveLimit, err := env.GetOption(mdbx.OptDpReverseLimit)
	if err != nil {
		panic(err)
	}
	if err = env.SetOption(mdbx.OptDpReverseLimit, dpReserveLimit*2); err != nil {
		panic(err)
	}

	defaultDirtyPagesLimit, err := env.GetOption(mdbx.OptTxnDpLimit)
	if err != nil {
		panic(err)
	}
	if err = env.SetOption(mdbx.OptTxnDpLimit, defaultDirtyPagesLimit*2); err != nil { // default is RAM/42
		panic(err)
	}

	if err := env.SetGeometry(-1, -1, 1<<43, 1<<30, -1, 1<<14); err != nil {
		panic(err)
	}

	if err = env.Open(path, uint(defaultFlags), 0664); err != nil {
		panic(err)
	}

	d := &MdbxDB{
		logger: logger,
		path:   path,
		dbi:    make(map[string]mdbx.DBI),
		syncCh: make(chan struct{}, 10240),
		stopCh: make(chan struct{}),
	}
	d.env = env

	d.acache = New(1 << 24)
	d.bcache = New(1 << 28)

	env.Update(func(txn *mdbx.Txn) error {
		// create or open all dbi
		for _, dbiName := range dbis {
			dbi, err := txn.CreateDBI(dbiName)
			if err != nil {
				panic(err)
			}
			d.dbi[dbiName] = dbi
		}
		return nil

	})
	var ce error
	d.cache, ce = lru.NewWithEvict(cacheSize, func(key string, value *NewValue) {
		if d.mu.TryLock() {
			d.bcache.Put(helper.S2B(key), value.Val)
			if d.acache.Size() != 0 {
				iter := d.acache.NewIterator()
				for iter.Next() {
					d.bcache.Put(iter.key, iter.value)
				}
				iter.Release()
				d.acache.Reset()
			}
			d.mu.Unlock()
		} else {
			d.acache.Put(helper.S2B(key), value.Val)
		}
		d.syncCh <- struct{}{}
	})
	if ce != nil {
		panic(ce)
	}

	go d.synccache()

	return d
}

func (d *MdbxDB) flush(allFlush bool) {
	var (
		info  *mdbx.EnvInfo
		count uint64
		err   error
		nv    *NewValue
		ok    bool
	)
	d.mu.Lock()
	defer d.mu.Unlock()

	stx := time.Now()
	d.env.Update(func(txn *mdbx.Txn) error {
		// flush lru cache
		keys := d.cache.Keys()
		for _, key := range keys {

			nv, ok = d.cache.Get(key)
			if !ok {
				continue
			}

			// if key exists, then skip
			if !allFlush {
				_, err = txn.Get(d.dbi[nv.Dbi], nv.Key)
				if err == nil {
					continue
				}
			}

			err = txn.Put(d.dbi[nv.Dbi], nv.Key, nv.Val, 0)
			if err != nil {
				panic(err)
			}

			count++
		}
		// flush bcache
		iter := d.bcache.NewIterator()
		for iter.Next() {
			err := txn.Put(d.dbi[helper.B2S(iter.key[:4])], iter.key[4:], iter.value, 0)
			if err != nil {
				panic(err)
			}
			count++
		}
		iter.Release()
		d.bcache.Reset()
		info, _ = d.env.Info(txn)
		return nil
	})
	d.logger.Info("flush", "keys", count, "elapse", time.Since(stx), "readers", info.NumReaders, "sync since", info.SinceSync)
}

func (d *MdbxDB) synccache() {
	var cnt uint64
	for {
		select {
		case <-d.stopCh:
			return
		case <-d.syncCh:
			atomic.AddUint64(&cnt, 1)
			if cnt%5120 == 0 {
				d.flush(false)
			}
		}
	}
}
