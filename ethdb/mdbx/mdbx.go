package mdbx

import (
	"bytes"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

type NewValue struct {
	Dbi string
	Key []byte
	Val []byte
}

type MdbxDB struct {
	path     string
	env      *mdbx.Env
	cache    *lru.Cache[string, *NewValue]
	secCache *lru.Cache[string, *NewValue]
	dbi      map[string]mdbx.DBI
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
		ethdb.TDDBI,
		ethdb.ReceiptsDBI,
		ethdb.SnapDBI,
		ethdb.QueueDBI,
		ethdb.CodeDBI,
	}
)

const (
	firstLevelCacheSize = 2048
	secondCacheSize     = 10240
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
			dbi, err := txn.CreateDBI(dbiName)
			if err != nil {
				panic(err)
			}
			d.dbi[dbiName] = dbi
		}
		return nil

	})

	// flush data to database
	var ce error

	d.secCache, ce = lru.NewWithEvict(secondCacheSize, func(key string, value *NewValue) {

		err := d.env.View(func(txn *mdbx.Txn) error {
			_, err := txn.Get(d.dbi[value.Dbi], value.Key)
			return err
		})

		if err != nil {
			batch := d.Batch()
			batch.Set(value.Dbi, value.Key, value.Val)
			keys := d.secCache.Keys()
			for _, key := range keys {
				val, _ := d.secCache.Get(key)
				batch.Set(val.Dbi, val.Key, val.Val)
			}
			err = batch.Write()
			if err != nil {
				panic(err)
			}
		}
	})

	if ce != nil {
		panic(ce)
	}

	d.cache, ce = lru.NewWithEvict(firstLevelCacheSize, func(key string, value *NewValue) {
		d.secCache.Add(key, value)
	})
	if ce != nil {
		panic(ce)
	}

	return d
}
