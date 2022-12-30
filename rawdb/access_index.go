package rawdb

import (
	"github.com/ankr/dogesyncer/ethdb"
	"github.com/ankr/dogesyncer/types"
)

func ReadState(db ethdb.Database, root types.Hash) ([]byte, error) {
	rs, ok, err := db.Get(ethdb.TrieDBI, root.Bytes())
	if err != nil {
		return nil, err
	}
	if ok {
		return rs, nil
	}
	return nil, ethdb.ErrNotFound
}
