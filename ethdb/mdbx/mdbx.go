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
	logger hclog.Logger
	path   string
	env    *mdbx.Env
	cache  *MemDB
	dbi    map[string]mdbx.DBI
	stopCh chan struct{}
}

var (
	strbuf = sync.Pool{
		New: func() any {
			return bytes.NewBuffer([]byte{})
		},
	}

	defaultFlags = mdbx.Durable | mdbx.NoReadahead | mdbx.Coalesce | mdbx.NoMemInit

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
		ethdb.TxLookUpDBI,
	}
)

const (
	cacheSize = 1 << 28
)

func NewMDBX(path string, logger hclog.Logger) *MdbxDB {

	env, err := mdbx.NewEnv()
	if err != nil {
		panic(err)
	}

	if err := env.SetOption(mdbx.OptMaxDB, 512); err != nil {
		panic(err)
	}

	if err := env.SetGeometry(-1, -1, 2*(1<<40), 1<<21, -1, 1<<12); err != nil {
		panic(err)
	}

	if err := env.SetOption(mdbx.OptRpAugmentLimit, 0x7fffFFFF); err != nil {
		panic(err)
	}

	if err := env.SetOption(mdbx.OptMaxReaders, 32000); err != nil {
		panic(err)
	}

	// open database
	if err = env.Open(path, uint(defaultFlags), 0664); err != nil {
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

	if err = env.SetSyncPeriod(3 * time.Second); err != nil {
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

	d := &MdbxDB{
		path:   path,
		logger: logger,
		dbi:    make(map[string]mdbx.DBI),
		stopCh: make(chan struct{}),
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

	return d
}

func (d *MdbxDB) syncCache() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	keyNums := 0
	stx := time.Now()
	tx, err := d.env.BeginTxn(nil, 0)
	if err != nil {
		return
	}
	iter := d.cache.NewIterator(nil)
	for iter.Next() {
		dbiName := helper.B2S(iter.key[:4])
		tx.Put(d.dbi[dbiName], iter.key[4:], iter.value, 0)
		keyNums++
	}
	inf, _ := d.env.Info(tx)
	tx.Commit()
	d.env.Sync(true, false)
	iter.Release()
	d.cache.Reset()

	d.logger.Info("stats",
		"elapse", time.Since(stx),
		"commit keys", keyNums,
		"max readers", inf.MaxReaders,
		"auto sync", inf.AutosyncPeriod,
		"since sync", inf.SinceSync,
		"num readers", inf.NumReaders)
}
