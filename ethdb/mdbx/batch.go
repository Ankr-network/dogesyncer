package mdbx

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

	var err error
	for _, keyvalue := range b.writes {
		err = b.db.Set(keyvalue.dbi, keyvalue.key, keyvalue.value)
		if err != nil {
			panic(err)
		}
	}

	return nil
}
