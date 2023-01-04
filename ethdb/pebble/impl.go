package pebble

import (
	"github.com/ankr/dogesyncer/ethdb"
	"github.com/cockroachdb/pebble"
	"github.com/sunvim/utils/cachem"
)

func (p *pebbleDB) Set(dbi string, k []byte, v []byte) error {
	key := cachem.Malloc(len(k) + 1)
	key[0] = pkmap[dbi][0]
	key = append(key, k...)
	err := p.d.Set(key, v, p.wo)
	cachem.Free(key)
	return err
}

func (p *pebbleDB) Get(dbi string, k []byte) ([]byte, bool, error) {

	key := cachem.Malloc(len(k) + 1)
	key[0] = pkmap[dbi][0]
	key = append(key, k...)

	grs, rclose, err := p.d.Get(key)
	cachem.Free(key)

	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, false, ethdb.ErrNotFound
		}
		return nil, false, err
	}
	rs := make([]byte, len(grs))
	copy(rs, grs)
	rclose.Close()

	return rs, true, nil
}

func (p *pebbleDB) Close() error {
	err := p.d.Flush()
	if err != nil {
		p.logger.Error("flush cache", "err", err)
	}
	err = p.d.Close()
	return err
}

func (p *pebbleDB) Remove(dbi string, k []byte) error {
	key := cachem.Malloc(len(k) + 1)
	key[0] = pkmap[dbi][0]
	key = append(key, k...)
	p.d.Delete(key, p.wo)
	cachem.Free(key)
	return nil
}

func (p *pebbleDB) Sync() error {
	return p.d.Flush()
}

func (p *pebbleDB) Batch() ethdb.Batch {
	return &pebbleBatch{
		b:  p.d.NewBatch(),
		wo: p.wo,
	}
}

type pebbleBatch struct {
	b  *pebble.Batch
	wo *pebble.WriteOptions
}

func (b *pebbleBatch) Set(dbi string, k, v []byte) error {
	key := cachem.Malloc(len(k) + 1)
	key[0] = pkmap[dbi][0]
	key = append(key, k...)
	err := b.b.Set(key, v, nil)
	cachem.Free(key)
	return err
}

func (b *pebbleBatch) Write() error {
	err := b.b.Commit(b.wo)
	b.b.Close()
	return err
}
