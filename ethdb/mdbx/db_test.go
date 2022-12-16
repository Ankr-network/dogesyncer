package mdbx

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/sunvim/dogesyncer/ethdb/dbtest"
)

func TestMdbxDB(t *testing.T) {
	t.Run("DatabaseSuite", func(t *testing.T) {
		dbtest.TestDatabaseSuite(t, func() ethdb.Database {
			db := NewMDBX(t.TempDir(), hclog.New(nil))
			return db
		})
	})
}
