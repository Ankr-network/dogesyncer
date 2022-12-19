package mdbx

import (
	"github.com/torquem-ch/mdbx-go/mdbx"
)

// KVBatch is a batch write for leveldb
type KVBatch struct {
	dbi map[string]mdbx.DBI
	db  *MdbxDB
}

func (b *KVBatch) Set(dbi string, k, v []byte) error {
	b.db.Set(dbi, k, v)
	return nil
}

func (b *KVBatch) Write() error {
	return nil
}
