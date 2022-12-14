package mdbx

import (
	"bytes"
	"runtime"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/sunvim/dogesyncer/helper"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

type NewValue struct {
	Dbi string
	Val []byte
}

type MdbxDB struct {
	logger  hclog.Logger
	path    string
	env     *mdbx.Env
	cache   *MemDB
	bkCache *MemDB
	dbi     map[string]mdbx.DBI
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

	mdbBuf = sync.Pool{
		New: func() any {
			return New(1 << 20)
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
		ethdb.TDDBI,
		ethdb.ReceiptsDBI,
		ethdb.SnapDBI,
		ethdb.CodeDBI,
	}
)

const (
	cacheSize = 1 << 29
)

func NewMDBX(path string) *MdbxDB {

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
		path: path,
		dbi:  make(map[string]mdbx.DBI),
	}
	d.env = env

	env.Update(func(txn *mdbx.Txn) error {
		// create or open all dbi
		for _, dbiName := range dbis {
			dbi, err := txn.CreateDBI(string(dbiName))
			if err != nil {
				panic(err)
			}
			d.dbi[string(dbiName)] = dbi
		}
		return nil

	})

	d.cache = New(cacheSize)
	d.bkCache = New(cacheSize)

	go d.syncPeriod()

	return d
}

func (d *MdbxDB) syncPeriod() {
	tick := time.Tick(45 * time.Second)
	for range tick {

		d.cache, d.bkCache = d.bkCache, d.cache

		runtime.LockOSThread()
		tx, err := d.env.BeginTxn(nil, 0)
		if err != nil {
			panic(err)
		}
		iter := d.bkCache.NewIterator(nil)
		for iter.Next() {
			dbiName := helper.B2S(iter.key[:4])
			_, err = tx.Get(d.dbi[dbiName], iter.key[4:])
			if err == nil {
				continue
			}
			tx.Put(d.dbi[dbiName], iter.key[4:], iter.value, 0)
		}
		tx.Commit()
		runtime.UnlockOSThread()
		iter.Release()

		d.bkCache.Reset()
	}
}
