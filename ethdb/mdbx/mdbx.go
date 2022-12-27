package mdbx

import (
	"bytes"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

type NewValue struct {
	Dbi string
	Key []byte
	Val []byte
}

type MdbxDB struct {
	mu     sync.Mutex
	logger hclog.Logger
	path   string
	env    *mdbx.Env
	dbi    map[string]mdbx.DBI
}

var (
	strbuf = sync.Pool{
		New: func() any {
			return bytes.NewBuffer([]byte{})
		},
	}

	memsize   = 1 << 28
	cachePool = &sync.Pool{
		New: func() any {
			return New(memsize)
		},
	}

	defaultFlags = mdbx.Durable | mdbx.NoReadahead | mdbx.Coalesce | mdbx.NoMetaSync

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

func NewMDBX(path string, logger hclog.Logger) *MdbxDB {

	env, err := mdbx.NewEnv()
	if err != nil {
		panic(err)
	}

	if err := env.SetOption(mdbx.OptMaxDB, 32); err != nil {
		panic(err)
	}

	if err := env.SetOption(mdbx.OptMaxReaders, 32000); err != nil {
		panic(err)
	}

	if err := env.SetGeometry(-1, -1, 1<<43, 1<<30, 1<<31, 1<<14); err != nil {
		panic(err)
	}

	if err = env.Open(path, uint(defaultFlags), 0664); err != nil {
		panic(err)
	}

	d := &MdbxDB{
		logger: logger,
		path:   path,
		dbi:    make(map[string]mdbx.DBI),
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

	return d
}
