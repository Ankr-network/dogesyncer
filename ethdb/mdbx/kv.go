package mdbx

import (
	"bytes"

	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/torquem-ch/mdbx-go/mdbx"
)

func (d *MdbxDB) Set(dbi string, k []byte, v []byte) error {
	buf := strbuf.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(dbi)
	buf.Write(k)
	d.cache.Put(buf.Bytes(), v)
	strbuf.Put(buf)

	return nil
}

func (d *MdbxDB) Get(dbi string, k []byte) ([]byte, bool, error) {

	buf := strbuf.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(dbi)
	buf.Write(k)

	// query from cache
	nvs, err := d.cache.Get(buf.Bytes())
	strbuf.Put(buf)
	if err == nil {
		return nvs, true, nil
	}

	var (
		v []byte
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
		return nil, false, nil
	}

	return v, true, nil
}

func (d *MdbxDB) Sync() error {
	d.syncCache()
	return nil
}

func (d *MdbxDB) Close() error {
	// flush cache data to database
	close(d.stopCh)
	d.syncCache()
	d.logger.Info("sync cache over")

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
