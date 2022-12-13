package mdbx

import (
	"runtime"

	"github.com/torquem-ch/mdbx-go/mdbx"
)

type keyvalue struct {
	dbi   string
	key   []byte
	value []byte
}

// KVBatch is a batch write for leveldb
type KVBatch struct {
	env    *mdbx.Env
	dbi    map[string]mdbx.DBI
	writes []keyvalue
}

func copyBytes(b []byte) (copiedBytes []byte) {
	if b == nil {
		return nil
	}
	copiedBytes = make([]byte, len(b))
	copy(copiedBytes, b)

	return
}

func (b *KVBatch) Set(dbi string, k, v []byte) error {
	b.writes = append(b.writes, keyvalue{dbi, copyBytes(k), copyBytes(v)})
	return nil
}

// why no error handle
func (b *KVBatch) Write() error {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	tx, err := b.env.BeginTxn(nil, 0)
	if err != nil {
		panic(err)
	}

	defer func() {
		tx.Commit()
	}()

	for _, keyvalue := range b.writes {
		err = tx.Put(b.dbi[keyvalue.dbi], keyvalue.key, keyvalue.value, 0)
		if err != nil {
			panic(err)
		}
	}

	return nil
}
