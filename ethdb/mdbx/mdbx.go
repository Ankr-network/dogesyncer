package mdbx

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"sync"
	"time"

	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/sunvim/dogesyncer/helper"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

type NewValue struct {
	life int64
	Val  []byte
}

func (nv *NewValue) Marshal() []byte {
	rs := make([]byte, 8)
	binary.BigEndian.PutUint64(rs, uint64(nv.life))
	rs = append(rs, nv.Val...)
	return rs
}

func (nv *NewValue) Unmarshal(input []byte) error {
	life := binary.BigEndian.Uint64(input[:8])
	nv.life = int64(life)
	nv.Val = input[8:]
	return nil
}

func (nv *NewValue) Reset() {
	nv.life = 0
	nv.Val = nil
}

type MdbxDB struct {
	path  string
	env   *mdbx.Env
	cache *MemDB
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

	go d.syncPeriod()

	return d
}

func (d *MdbxDB) syncPeriod() {
	tick := time.Tick(2 * time.Minute)
	for range tick {

		var keys [][]byte

		runtime.LockOSThread()
		tx, err := d.env.BeginTxn(nil, 0)
		if err != nil {
			panic(err)
		}
		iter := d.cache.NewIterator(nil)
		for iter.Next() {
			dbiName := helper.B2S(iter.key[:4])
			_, err = tx.Get(d.dbi[dbiName], iter.key[4:])
			if err == nil {
				keys = append(keys, iter.key[4:])
				continue
			}
			tx.Put(d.dbi[dbiName], iter.key[4:], iter.value, 0)
		}

		tx.Commit()
		runtime.UnlockOSThread()

		for _, key := range keys {
			d.cache.Delete(key)
			runtime.Gosched()
		}

		keys = nil
	}
}
