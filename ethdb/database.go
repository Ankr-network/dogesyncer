package ethdb

import "fmt"

var (
	TrieDBI     = "trie"
	BodyDBI     = "blck"
	HeadDBI     = "head"
	AssistDBI   = "assi"
	NumHashDBI  = "nuha"
	TxesDBI     = "txes"
	TxLookUpDBI = "lkup"
	ReceiptsDBI = "rept"
	TODBI       = "todi" // total difficulty
	SnapDBI     = "snap" // consensus snapshot
	CodeDBI     = "code" // save contract code
)

var (
	ErrNotFound = fmt.Errorf("Not Found")
)

type Setter interface {
	Set(dbi string, k, v []byte) error
}

type Getter interface {
	Get(dbi string, k []byte) ([]byte, bool, error)
}

type Batch interface {
	Setter
	Write() error
}

type Closer interface {
	Close() error
}

type Remover interface {
	Remove(dbi string, k []byte) error
}

type Syncer interface {
	Sync() error
}

type Database interface {
	Setter
	Getter
	Closer
	Remover
	Syncer
	Batch() Batch
}
