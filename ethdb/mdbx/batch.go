package mdbx

import (
	"bytes"
	"runtime"

	"github.com/sunvim/dogesyncer/helper"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

// KVBatch is a batch write for leveldb
type KVBatch struct {
	env *mdbx.Env
	dbi map[string]mdbx.DBI
	db  *MemDB
}

func (b *KVBatch) Set(dbi string, k, v []byte) error {

	buf := strbuf.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(dbi)
	buf.Write(k)
	b.db.Put(buf.Bytes(), v)
	strbuf.Put(buf)

	return nil
}

// why no error handle
func (b *KVBatch) Write() error {

	runtime.LockOSThread()
	tx, err := b.env.BeginTxn(nil, 0)
	if err != nil {
		panic(err)
	}
	iter := b.db.NewIterator(nil)
	for iter.Next() {
		err = tx.Put(b.dbi[helper.B2S(iter.key[:4])], iter.key[4:], iter.value, 0)
		if err != nil {
			panic(err)
		}
	}

	tx.Commit()
	runtime.UnlockOSThread()

	mdbBuf.Put(b.db)

	return nil
}
