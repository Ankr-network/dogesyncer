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
	if dbi == ethdb.AssistDBI {
		d.asist.Set(buf.String(), nv)
	} else {
		d.cache.Set(buf.String(), nv)
	}
	strbuf.Put(buf)

	return nil
}

func (d *MdbxDB) Get(dbi string, k []byte) ([]byte, bool, error) {

	buf := strbuf.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(dbi)
	buf.Write(k)

	var (
		nv *NewValue
		ok bool
	)

	// query from cache
	if dbi == ethdb.AssistDBI {
		nv, ok = d.asist.Get(buf.String())
	} else {
		nv, ok = d.cache.Get(buf.String())
	}

	strbuf.Put(buf)

	if ok {
		return nv.Val, true, nil
	}

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
	batch := d.Batch()

	d.cache.Range(func(s string, nv *NewValue) bool {
		batch.Set(nv.Dbi, nv.Key, nv.Val)
		return true
	})

	d.asist.Range(func(s string, nv *NewValue) bool {
		batch.Set(nv.Dbi, nv.Key, nv.Val)
		return true
	})

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
