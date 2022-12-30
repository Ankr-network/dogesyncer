package pebble

import (
	"testing"

	"github.com/ankr/dogesyncer/ethdb"
	"github.com/hashicorp/go-hclog"
)

func TestSetGet(t *testing.T) {
	db := NewPebbleDB(t.TempDir(), hclog.New(nil))

	k := []byte("hello")
	v := []byte("world")

	err := db.Set(ethdb.BodyDBI, k, v)
	if err != nil {
		t.Error(err)
	}
	rs, ok, err := db.Get(ethdb.BodyDBI, k)
	if err != nil {
		t.Error(err)
	}
	t.Logf("val: %s ok: %v \n", rs, ok)
}
