package itrie

import (
	"errors"
	"fmt"
	"github.com/ankr/dogesyncer/blockchain"
	"github.com/ankr/dogesyncer/helper/keccak"

	lru "github.com/hashicorp/golang-lru"

	"github.com/ankr/dogesyncer/state"
	"github.com/ankr/dogesyncer/types"
)

const (
	codeLruCacheSize         = 8192
	trieStateLruCacheSize    = 2048
	accountStateLruCacheSize = 4096
)

type State struct {
	storage Storage

	codeLruCache      *lru.Cache
	trieStateCache    *lru.Cache
	accountStateCache *lru.Cache

	metrics *Metrics
}

func NewState(storage Storage, metrics *Metrics) *State {
	codeLruCache, _ := lru.New(codeLruCacheSize)
	trieStateCache, _ := lru.New(trieStateLruCacheSize)
	accountStateCache, _ := lru.New(accountStateLruCacheSize)

	s := &State{
		storage:           storage,
		trieStateCache:    trieStateCache,
		accountStateCache: accountStateCache,
		codeLruCache:      codeLruCache,
		metrics:           NewDummyMetrics(metrics),
	}

	return s
}

func (s *State) NewSnapshot() state.Snapshot {
	t := NewTrie()
	t.state = s
	t.storage = s.storage

	return t
}

func (s *State) SetCode(hash types.Hash, code []byte) error {
	err := s.storage.SetCode(hash, code)

	if err != nil {
		return err
	}

	s.codeLruCache.Add(hash, code)

	s.metrics.CodeLruCacheWrite.Add(1)

	return err
}

func (s *State) GetState(root types.Hash, slot []byte) ([]byte, error) {
	// the values in the trie are the hashed objects of the keys
	key := keccak.Keccak256(nil, slot)

	snap, err := s.NewSnapshotAt(root)
	if err != nil {
		return nil, err
	}

	result, ok := snap.Get(key)

	if !ok {
		return nil, blockchain.ErrStateNotFound
	}

	return result, nil
}

func (s *State) GetCode(hash types.Hash) ([]byte, bool) {
	defer s.metrics.CodeLruCacheRead.Add(1)

	// find code in cache
	if cacheCode, ok := s.codeLruCache.Get(hash); ok {
		if code, ok := cacheCode.([]byte); ok {
			s.metrics.CodeLruCacheHit.Add(1)

			return code, true
		}
	}

	s.metrics.CodeLruCacheMiss.Add(1)

	code, ok := s.storage.GetCode(hash)
	if ok {
		s.codeLruCache.Add(hash, code)

		s.metrics.CodeLruCacheWrite.Add(1)
	}

	return code, ok
}

func (s *State) NewSnapshotAt(root types.Hash) (state.Snapshot, error) {
	if root == types.EmptyRootHash {
		// empty state
		return s.NewSnapshot(), nil
	}

	tt, ok := s.trieStateCache.Get(root)
	if ok {
		trie, ok := tt.(*Trie)
		if !ok {
			return nil, errors.New("invalid type assertion")
		}

		s.metrics.TrieStateLruCacheHit.Add(1)

		return trie, nil
	}

	tt, ok = s.accountStateCache.Get(root)
	if ok {
		trie, ok := tt.(*Trie)
		if !ok {
			return nil, errors.New("invalid type assertion")
		}

		s.metrics.AccountStateLruCacheHit.Add(1)

		return trie, nil
	}

	n, ok, err := GetNode(root.Bytes(), s.storage)

	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, fmt.Errorf("state not found at hash %s", root)
	}

	s.metrics.StateLruCacheMiss.Add(1)

	t := &Trie{
		root:    n,
		state:   s,
		storage: s.storage,
	}

	return t, nil
}

func (s *State) AddAccountState(root types.Hash, t *Trie) {
	s.accountStateCache.Add(root, t)
}

func (s *State) AddTrieState(root types.Hash, t *Trie) {
	s.trieStateCache.Add(root, t)
}
