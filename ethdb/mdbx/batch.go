package mdbx

import (
	"bytes"

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

func (b *KVBatch) Write() error {
	return nil
}
