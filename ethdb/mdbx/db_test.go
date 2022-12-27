package mdbx

import (
	"testing"

	"github.com/ankr/dogesyncer/ethdb"
	"github.com/ankr/dogesyncer/ethdb/dbtest"
	"github.com/hashicorp/go-hclog"
)

func TestMdbxDB(t *testing.T) {
	t.Run("DatabaseSuite", func(t *testing.T) {
		dbtest.TestDatabaseSuite(t, func() ethdb.Database {
			db := NewMDBX(t.TempDir(), hclog.New(nil))
			return db
		})
	})
}
