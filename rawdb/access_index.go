package rawdb

import (
	"github.com/ankr/dogesyncer/ethdb"
	"github.com/ankr/dogesyncer/helper"
	"github.com/ankr/dogesyncer/types"
)

func WriteSnap(db ethdb.Database, number uint64, snap *types.Snapshot) error {
	out, err := snap.Marshal()
	if err != nil {
		return err
	}
	return db.Set(ethdb.SnapDBI, helper.EncodeVarint(number), out)
}

func ReadSnap(db ethdb.Database, number uint64) (*types.Snapshot, error) {
	var rs *types.Snapshot
	out, ok, err := db.Get(ethdb.SnapDBI, helper.EncodeVarint(number))
	if err != nil {
		return nil, err
	}
	if ok {
		err = rs.Unmarshal(out)
		if err != nil {
			return nil, err
		}
	}
	return rs, nil
}

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
