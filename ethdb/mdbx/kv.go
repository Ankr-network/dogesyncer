package mdbx

import (
	"bytes"
	"runtime"

	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/sunvim/dogesyncer/helper"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

func (d *MdbxDB) Set(dbi string, k []byte, v []byte) error {

	d.lock.Lock()
	buf := strbuf.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(dbi)
	buf.Write(k)
	if dbi == ethdb.AssistDBI {
		nv, ok := d.asist[buf.String()]
		if ok {
			nv.Val = v
		}
		d.asist[buf.String()] = &NewValue{Dbi: dbi, Val: v}
	} else {
		d.cache.Put(buf.Bytes(), v)
	}
	strbuf.Put(buf)
	d.lock.Unlock()

	return nil
}

func (d *MdbxDB) Get(dbi string, k []byte) ([]byte, bool, error) {

	buf := strbuf.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(dbi)
	buf.Write(k)

	// query from cache
	if dbi == ethdb.AssistDBI {
		nv, ok := d.asist[buf.String()]
		if ok {
			return nv.Val, true, nil
		}
	} else {
		nvs, err := d.cache.Get(buf.Bytes())
		strbuf.Put(buf)
		if err == nil {
			return nvs, true, nil
		}

		nvs, err = d.bkCache.Get(buf.Bytes())
		strbuf.Put(buf)
		if err == nil {
			return nvs, true, nil
		}
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
	runtime.LockOSThread()
	tx, err := d.env.BeginTxn(nil, 0)
	if err != nil {
		panic(err)
	}
	iter := d.cache.NewIterator(nil)
	for iter.Next() {
		tx.Put(d.dbi[helper.B2S(iter.key[:4])], iter.key[4:], iter.value, 0)
	}

	for k, v := range d.asist {
		tx.Put(d.dbi[v.Dbi], helper.S2B(k)[4:], v.Val, 0)
	}

	tx.Commit()
	runtime.UnlockOSThread()
	iter.Release()

	d.env.Sync(true, false)
	for _, dbi := range d.dbi {
		d.env.CloseDBI(dbi)
	}
	d.env.Close()
	return nil
}

func (d *MdbxDB) Batch() ethdb.Batch {
	return &KVBatch{env: d.env, dbi: d.dbi, db: d.cache}
}

func (d *MdbxDB) Remove(dbi string, k []byte) error {
	return d.env.Update(func(txn *mdbx.Txn) error {
		return txn.Del(d.dbi[dbi], k, nil)
	})
}
