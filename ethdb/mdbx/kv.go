package mdbx

import (
	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

func (d *MdbxDB) Set(dbi string, k []byte, v []byte) error {
	return d.env.Update(func(txn *mdbx.Txn) error {
		return txn.Put(d.dbi[dbi], k, v, 0)
	})
}

func (d *MdbxDB) Get(dbi string, k []byte) ([]byte, bool, error) {
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
	d.env.Sync(true, false)
	for _, dbi := range d.dbi {
		d.env.CloseDBI(dbi)
	}
	d.env.Close()
	return nil
}

func (d *MdbxDB) Batch() ethdb.Batch {
	return &KVBatch{db: d}
}

func (d *MdbxDB) Remove(dbi string, k []byte) error {
	return d.env.Update(func(txn *mdbx.Txn) error {
		return txn.Del(d.dbi[dbi], k, nil)
	})
}
