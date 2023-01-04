package pebble

import "github.com/ankr/dogesyncer/ethdb"

var (
	pkmap = map[string][]byte{
		ethdb.AssistDBI:   []byte("a"),
		ethdb.BodyDBI:     []byte("b"),
		ethdb.CodeDBI:     []byte("c"),
		ethdb.HeadDBI:     []byte("h"),
		ethdb.NumHashDBI:  []byte("n"),
		ethdb.TODBI:       []byte("t"),
		ethdb.TrieDBI:     []byte("r"),
		ethdb.TxLookUpDBI: []byte("l"),
		ethdb.ReceiptsDBI: []byte("s"),
		ethdb.TxesDBI:     []byte("x"),
	}
)
