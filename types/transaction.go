package types

import (
	"container/heap"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/dogechain-lab/fastrlp"
	"github.com/sunvim/dogesyncer/helper/keccak"
)

type Transaction struct {
	Nonce    uint64
	GasPrice *big.Int
	Gas      uint64
	To       *Address
	Value    *big.Int
	Input    []byte
	V        *big.Int
	R        *big.Int
	S        *big.Int
	From     Address

	// Cache
	size atomic.Value
	hash atomic.Value

	// time at which the node received the tx
	ReceivedTime time.Time
}

func (t *Transaction) IsContractCreation() bool {
	return t.To == nil
}

func (t *Transaction) Hash() Hash {
	if hash := t.hash.Load(); hash != nil {
		//nolint:forcetypeassert
		return hash.(Hash)
	}

	hash := t.rlpHash()
	t.hash.Store(hash)

	return hash
}

// rlpHash encodes transaction hash.
func (t *Transaction) rlpHash() (h Hash) {
	ar := &fastrlp.Arena{}
	hash := keccak.DefaultKeccakPool.Get()

	v := t.MarshalRLPWith(ar)
	hash.WriteRlp(h[:0], v)
	keccak.DefaultKeccakPool.Put(hash)

	return h
}

// Copy returns a deep copy
func (t *Transaction) Copy() *Transaction {
	tt := &Transaction{
		Nonce: t.Nonce,
		Gas:   t.Gas,
		From:  t.From,
	}

	tt.GasPrice = new(big.Int)
	if t.GasPrice != nil {
		tt.GasPrice.Set(t.GasPrice)
	}

	if t.To != nil {
		toAddr := *t.To
		tt.To = &toAddr
	}

	tt.Value = new(big.Int)
	if t.Value != nil {
		tt.Value.Set(t.Value)
	}

	if len(t.Input) > 0 {
		tt.Input = make([]byte, len(t.Input))
		copy(tt.Input[:], t.Input[:])
	}

	if t.V != nil {
		tt.V = new(big.Int).SetBits(t.V.Bits())
	}

	if t.R != nil {
		tt.R = new(big.Int).SetBits(t.R.Bits())
	}

	if t.S != nil {
		tt.S = new(big.Int).SetBits(t.S.Bits())
	}

	tt.ReceivedTime = t.ReceivedTime

	return tt
}

// Cost returns gas * gasPrice + value
func (t *Transaction) Cost() *big.Int {
	total := new(big.Int).Mul(t.GasPrice, new(big.Int).SetUint64(t.Gas))
	total.Add(total, t.Value)

	return total
}

func (t *Transaction) Size() uint64 {
	if size := t.size.Load(); size != nil {
		sizeVal, ok := size.(uint64)
		if !ok {
			return 0
		}

		return sizeVal
	}

	size := uint64(len(t.MarshalRLP()))
	t.size.Store(size)

	return size
}

func (t *Transaction) ExceedsBlockGasLimit(blockGasLimit uint64) bool {
	return t.Gas > blockGasLimit
}

func (t *Transaction) IsUnderpriced(priceLimit uint64) bool {
	return t.GasPrice.Cmp(big.NewInt(0).SetUint64(priceLimit)) < 0
}

func (t *Transaction) MarshalRLP() []byte {
	return t.MarshalRLPTo(nil)
}

func (t *Transaction) MarshalRLPTo(dst []byte) []byte {
	return MarshalRLPTo(t.MarshalRLPWith, dst)
}

// MarshalRLPWith marshals the transaction to RLP with a specific fastrlp.Arena
func (t *Transaction) MarshalRLPWith(arena *fastrlp.Arena) *fastrlp.Value {
	vv := arena.NewArray()

	vv.Set(arena.NewUint(t.Nonce))
	vv.Set(arena.NewBigInt(t.GasPrice))
	vv.Set(arena.NewUint(t.Gas))

	// Address may be empty
	if t.To != nil {
		vv.Set(arena.NewBytes((*t.To).Bytes()))
	} else {
		vv.Set(arena.NewNull())
	}

	vv.Set(arena.NewBigInt(t.Value))
	vv.Set(arena.NewCopyBytes(t.Input))

	// signature values
	vv.Set(arena.NewBigInt(t.V))
	vv.Set(arena.NewBigInt(t.R))
	vv.Set(arena.NewBigInt(t.S))

	return vv
}

// TxByPriceAndTime implements both the sort and the heap interface, making it useful
// for all at once sorting as well as individually adding and removing elements.
type TxByPriceAndTime []*Transaction

func (s TxByPriceAndTime) Len() int {
	return len(s)
}

func (s TxByPriceAndTime) Less(i, j int) bool {
	// If the prices are equal, use the time the transaction was first seen for deterministic sorting
	cmp := s[i].GasPrice.Cmp(s[j].GasPrice)
	if cmp == 0 {
		return s[i].ReceivedTime.Before(s[j].ReceivedTime)
	}

	return cmp > 0
}

func (s TxByPriceAndTime) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s *TxByPriceAndTime) Push(x interface{}) {
	if v, ok := x.(*Transaction); ok {
		*s = append(*s, v)
	}
}

func (s *TxByPriceAndTime) Pop() interface{} {
	old := *s
	n := len(old)
	x := old[n-1]
	*s = old[0 : n-1]

	return x
}

// PoolTxByNonce implements the sort interface to allow sorting a list of transactions
// by their nonces. This is usually only useful for sorting transactions from a
// single account, otherwise a nonce comparison doesn't make much sense.
type PoolTxByNonce []*Transaction

func (s PoolTxByNonce) Len() int           { return len(s) }
func (s PoolTxByNonce) Less(i, j int) bool { return (s[i]).Nonce < (s[j]).Nonce }
func (s PoolTxByNonce) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// TransactionsByPriceAndNonce represents a set of transactions that can return
// transactions in a profit-maximizing sorted order, while supporting removing
// entire batches of transactions for non-executable accounts.
type TransactionsByPriceAndNonce struct {
	txs   map[Address][]*Transaction // Per account nonce-sorted list of transactions
	heads TxByPriceAndTime           // Next transaction for each unique account (price heap)
}

// NewTransactionsByPriceAndNonce creates a transaction set that can retrieve
// price sorted transactions in a nonce-honouring way.
//
// Note, the input map is reowned so the caller should not interact any more with
// if after providing it to the constructor.
func NewTransactionsByPriceAndNonce(txs map[Address][]*Transaction) *TransactionsByPriceAndNonce {
	// Initialize a price and received time based heap with the head transactions
	heads := make(TxByPriceAndTime, 0, len(txs))

	for from, accTxs := range txs {
		heads = append(heads, accTxs[0])
		txs[from] = accTxs[1:]
	}

	heap.Init(&heads)

	// Assemble and return the transaction set
	return &TransactionsByPriceAndNonce{
		txs:   txs,
		heads: heads,
	}
}

// Peek returns the next transaction by price.
func (t *TransactionsByPriceAndNonce) Peek() *Transaction {
	if len(t.heads) == 0 {
		return nil
	}

	return t.heads[0]
}

// Shift replaces the current best head with the next one from the same account.
func (t *TransactionsByPriceAndNonce) Shift() {
	account := t.heads[0].From
	if txs, ok := t.txs[account]; ok && len(txs) > 0 {
		t.heads[0], t.txs[account] = txs[0], txs[1:]
		heap.Fix(&t.heads, 0)

		return
	}

	heap.Pop(&t.heads)
}

// Pop removes the best transaction, *not* replacing it with the next one from
// the same account. This should be used when a transaction cannot be executed
// and hence all subsequent ones should be discarded from the same account.
func (t *TransactionsByPriceAndNonce) Pop() {
	heap.Pop(&t.heads)
}

func (t *Transaction) UnmarshalRLP(input []byte) error {
	return UnmarshalRlp(t.UnmarshalRLPFrom, input)
}

// UnmarshalRLP unmarshals a Transaction in RLP format
func (t *Transaction) UnmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	elems, err := v.GetElems()
	if err != nil {
		return err
	}

	if len(elems) < 9 {
		return fmt.Errorf("incorrect number of elements to decode transaction, expected at least 9 but found %d",
			len(elems))
	}

	// nonce
	if t.Nonce, err = elems[0].GetUint64(); err != nil {
		return err
	}
	// gasPrice
	t.GasPrice = new(big.Int)
	if err := elems[1].GetBigInt(t.GasPrice); err != nil {
		return err
	}
	// gas
	if t.Gas, err = elems[2].GetUint64(); err != nil {
		return err
	}
	// to
	if vv, _ := v.Get(3).Bytes(); len(vv) == 20 {
		// address
		addr := BytesToAddress(vv)
		t.To = &addr
	} else {
		// reset To
		t.To = nil
	}
	// value
	t.Value = new(big.Int)
	if err := elems[4].GetBigInt(t.Value); err != nil {
		return err
	}
	// input
	if t.Input, err = elems[5].GetBytes(t.Input[:0]); err != nil {
		return err
	}

	// V
	t.V = new(big.Int)
	if err = elems[6].GetBigInt(t.V); err != nil {
		return err
	}

	// R
	t.R = new(big.Int)
	if err = elems[7].GetBigInt(t.R); err != nil {
		return err
	}
	// S
	t.S = new(big.Int)
	if err = elems[8].GetBigInt(t.S); err != nil {
		return err
	}

	// cache hash
	t.Hash()

	return nil
}
