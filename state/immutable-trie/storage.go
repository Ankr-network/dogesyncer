package itrie

import (
	"fmt"

	"github.com/ankr/dogesyncer/ethdb"
	"github.com/ankr/dogesyncer/helper/hex"
	"github.com/ankr/dogesyncer/types"
	"github.com/dogechain-lab/fastrlp"
)

var parserPool fastrlp.ParserPool

type Storage interface {
	Set(k, v []byte) error
	Get(k []byte) ([]byte, bool, error)

	SetCode(hash types.Hash, code []byte) error
	GetCode(hash types.Hash) ([]byte, bool)
	Close() error

	Batch() ethdb.Batch
}

type KVStorage struct {
	db ethdb.Database
}

func NewKVStorage(db ethdb.Database) Storage {
	return &KVStorage{db}
}
func (kv *KVStorage) Set(k []byte, v []byte) error {

	return kv.db.Set(ethdb.TrieDBI, k, v)
}

func (kv *KVStorage) Get(k []byte) ([]byte, bool, error) {
	return kv.db.Get(ethdb.TrieDBI, k)
}

func (kv *KVStorage) Close() error {
	return nil
}

func (kv *KVStorage) Batch() ethdb.Batch {
	return kv.db.Batch()
}

func (kv *KVStorage) SetCode(hash types.Hash, code []byte) error {
	return kv.db.Set(ethdb.CodeDBI, hash.Bytes(), code)
}

func (kv *KVStorage) GetCode(hash types.Hash) ([]byte, bool) {
	rs, ok, _ := kv.db.Get(ethdb.CodeDBI, hash.Bytes())
	if !ok {
		return nil, false
	}
	return rs, ok
}

type memStorage struct {
	db   map[string][]byte
	code map[string][]byte
}

type memBatch struct {
	db *map[string][]byte
}

// NewMemoryStorage creates an inmemory trie storage
func NewMemoryStorage() Storage {
	return &memStorage{db: map[string][]byte{}, code: map[string][]byte{}}
}

func (m *memStorage) Set(p []byte, v []byte) error {
	buf := make([]byte, len(v))
	copy(buf[:], v[:])
	m.db[hex.EncodeToHex(p)] = buf

	return nil
}

func (m *memStorage) Sync() error {
	return nil
}

func (m *memStorage) Get(p []byte) ([]byte, bool, error) {
	v, ok := m.db[hex.EncodeToHex(p)]
	if !ok {
		return []byte{}, false, nil
	}

	return v, true, nil
}

func (m *memStorage) SetCode(hash types.Hash, code []byte) error {
	m.code[hash.String()] = code

	return nil
}

func (m *memStorage) GetCode(hash types.Hash) ([]byte, bool) {
	code, ok := m.code[hash.String()]

	return code, ok
}

func (m *memStorage) Batch() ethdb.Batch {
	return &memBatch{db: &m.db}
}

func (m *memStorage) Close() error {
	return nil
}

func (m *memBatch) Set(dbi string, p, v []byte) error {
	buf := make([]byte, len(v))
	copy(buf[:], v[:])
	(*m.db)[hex.EncodeToHex(p)] = buf
	return nil
}

func (m *memBatch) Write() error {
	return nil
}

// GetNode retrieves a node from storage
func GetNode(root []byte, storage Storage) (Node, bool, error) {
	data, ok, _ := storage.Get(root)
	if !ok {
		return nil, false, nil
	}

	// NOTE. We dont need to make copies of the bytes because the nodes
	// take the reference from data itself which is a safe copy.
	p := parserPool.Get()
	defer parserPool.Put(p)

	v, err := p.Parse(data)
	if err != nil {
		return nil, false, err
	}

	if v.Type() != fastrlp.TypeArray {
		return nil, false, fmt.Errorf("storage item should be an array")
	}

	n, err := decodeNode(v, storage)

	return n, err == nil, err
}

func decodeNode(v *fastrlp.Value, s Storage) (Node, error) {
	if v.Type() == fastrlp.TypeBytes {
		vv := &ValueNode{
			hash: true,
		}
		vv.buf = append(vv.buf[:0], v.Raw()...)

		return vv, nil
	}

	var err error

	// TODO remove this once 1.0.4 of ifshort is merged in golangci-lint
	ll := v.Elems() //nolint:ifshort
	if ll == 2 {
		key := v.Get(0)
		if key.Type() != fastrlp.TypeBytes {
			return nil, fmt.Errorf("short key expected to be bytes")
		}

		// this can be either an array (extension node)
		// or bytes (leaf node)
		nc := &ShortNode{}
		nc.key = decodeCompact(key.Raw())

		if hasTerminator(nc.key) {
			// value node
			if v.Get(1).Type() != fastrlp.TypeBytes {
				return nil, fmt.Errorf("short leaf value expected to be bytes")
			}

			vv := &ValueNode{}
			vv.buf = append(vv.buf, v.Get(1).Raw()...)
			nc.child = vv
		} else {
			nc.child, err = decodeNode(v.Get(1), s)
			if err != nil {
				return nil, err
			}
		}

		return nc, nil
	} else if ll == 17 {
		// full node
		nc := &FullNode{}
		for i := 0; i < 16; i++ {
			if v.Get(i).Type() == fastrlp.TypeBytes && len(v.Get(i).Raw()) == 0 {
				// empty
				continue
			}
			nc.children[i], err = decodeNode(v.Get(i), s)
			if err != nil {
				return nil, err
			}
		}

		if v.Get(16).Type() != fastrlp.TypeBytes {
			return nil, fmt.Errorf("full node value expected to be bytes")
		}
		if len(v.Get(16).Raw()) != 0 {
			vv := &ValueNode{}
			vv.buf = append(vv.buf[:0], v.Get(16).Raw()...)
			nc.value = vv
		}

		return nc, nil
	}

	return nil, fmt.Errorf("node has incorrect number of leafs")
}
