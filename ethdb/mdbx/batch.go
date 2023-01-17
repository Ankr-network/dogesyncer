package mdbx

import (
	"github.com/sunvim/utils/cachem"
	"runtime"
)

type keyvalue struct {
	dbi   string
	key   []byte
	value []byte
}

// KVBatch is a batch write for leveldb
type KVBatch struct {
	writes []keyvalue
	db     *MdbxDB
}

func copyBytes(b []byte) (copiedBytes []byte) {
	if b == nil {
		return nil
	}
	copiedBytes = cachem.Malloc(len(b))
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

	var err error

	txn, err := b.db.env.BeginTxn(nil, 0)
	if err != nil {
		panic(err)
	}
	defer txn.Commit()

	for _, keyValue := range b.writes {
		err = txn.Put(b.db.dbi[keyValue.dbi], keyValue.key, keyValue.value, 0)
		if err != nil {
			panic(err)
		}

		cachem.Free(keyValue.key)
		cachem.Free(keyValue.value)
	}

	return nil
}
