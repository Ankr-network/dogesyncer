package mdbx

import (
	"bytes"
	"sync"
	"time"

	"github.com/cornelk/hashmap"
	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

type NewValue struct {
	Dbi string
	Key []byte
	Val []byte
}

func (nv *NewValue) Reset() {
	nv.Dbi = ""
	nv.Key = nil
	nv.Val = nil
}

type MdbxDB struct {
	path  string
	env   *mdbx.Env
	cache *hashmap.Map[string, *NewValue]
	dbi   map[string]mdbx.DBI
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

	d.cache = hashmap.New[string, *NewValue]()
	go d.syncPeriod()
	return d
}

func (d *MdbxDB) syncCache() {
	b := d.Batch()
	d.cache.Range(func(s string, nv *NewValue) bool {
		b.Set(nv.Dbi, nv.Key, nv.Val)
		d.cache.Del(s)
		return true
	})
	err := b.Write()
	if err != nil { // shouldn't miss data, so panic
		panic(err)
	}
}

func (d *MdbxDB) syncPeriod() {
	tick := time.Tick(30 * time.Second)
	for range tick {
		d.syncCache()
	}
}
