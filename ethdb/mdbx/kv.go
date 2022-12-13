package mdbx

import (
	"bytes"

	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

func (d *MdbxDB) Set(dbi string, k []byte, v []byte) error {

	nv := &NewValue{}
	nv.Dbi = dbi
	nv.Key = k
	nv.Val = v

	buf := strbuf.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(dbi)
	buf.Write(k)
	d.cache.Add(buf.String(), nv)
	strbuf.Put(buf)

	return nil
}

func (d *MdbxDB) Get(dbi string, k []byte) ([]byte, bool, error) {

	buf := strbuf.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(dbi)
	buf.Write(k)

	// query first level cache
	nv, ok := d.cache.Get(buf.String())
	if ok {
		return nv.Val, true, nil
	}

	// query second level cache
	nv, ok = d.secCache.Get(buf.String())
	if ok {
		return nv.Val, true, nil
	}
	strbuf.Put(buf)

	var (
		v []byte
		r bool
		e error
	)

	e = d.env.View(func(txn *mdbx.Txn) error {

		v, e = txn.Get(d.dbi[dbi], k)
		if e != nil {
			return e
		}
		return nil
	})

	if e != nil {
		if e == mdbx.NotFound {
			e = nil
			r = false
		}
	} else {
		r = true
	}

	return v, r, e
}

func (d *MdbxDB) Sync() error {
	d.env.Sync(true, false)
	return nil
}

func (d *MdbxDB) Close() error {
	// flush cache data to database
	keys := d.cache.Keys()
	batch := d.Batch()
	for _, key := range keys {
		nv, _ := d.cache.Get(key)
		batch.Set(nv.Dbi, nv.Key, nv.Val)
	}
	batch.Write()

	d.env.Sync(true, false)
	for _, dbi := range d.dbi {
		d.env.CloseDBI(dbi)
	}
	d.env.Close()
	return nil
}

func (d *MdbxDB) Batch() ethdb.Batch {
	return &KVBatch{env: d.env, dbi: d.dbi}
}

func (d *MdbxDB) Remove(dbi string, k []byte) error {
	return d.env.Update(func(txn *mdbx.Txn) error {
		return txn.Del(d.dbi[dbi], k, nil)
	})
}
