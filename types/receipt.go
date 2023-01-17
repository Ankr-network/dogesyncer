package types

import (
	"fmt"

	"github.com/ankr/dogesyncer/helper/keccak"
	"github.com/dogechain-lab/fastrlp"
)

type ReceiptStatus uint64

const (
	ReceiptFailed ReceiptStatus = iota
	ReceiptSuccess
)

type Receipts []*Receipt

type Receipt struct {
	// consensus fields
	Root              Hash
	CumulativeGasUsed uint64
	LogsBloom         Bloom
	Logs              []*Log
	Status            *ReceiptStatus

	// context fields
	GasUsed         uint64
	ContractAddress *Address
	TxHash          Hash
}

func (r *Receipt) SetStatus(s ReceiptStatus) {
	r.Status = &s
}

func (r *Receipt) SetContractAddress(contractAddress Address) {
	r.ContractAddress = &contractAddress
}

type Log struct {
	Address Address
	Topics  []Hash
	Data    []byte
}

func (l *Log) MarshalRLPWith(a *fastrlp.Arena) *fastrlp.Value {
	v := a.NewArray()
	v.Set(a.NewBytes(l.Address.Bytes()))

	topics := a.NewArray()
	for _, t := range l.Topics {
		topics.Set(a.NewBytes(t.Bytes()))
	}

	v.Set(topics)
	v.Set(a.NewBytes(l.Data))

	return v
}

func (b *Bloom) setEncode(hasher *keccak.Keccak, h []byte) {
	hasher.Reset()
	hasher.Write(h[:])
	buf := hasher.Read()

	for i := 0; i < 6; i += 2 {
		// Find the global bit location
		bit := (uint(buf[i+1]) + (uint(buf[i]) << 8)) & 2047

		// Find where the bit maps in the [0..255] byte array
		byteLocation := 256 - 1 - bit/8
		bitLocation := bit % 8
		b[byteLocation] = b[byteLocation] | (1 << bitLocation)
	}
}

// IsLogInBloom checks if the log has a possible presence in the bloom filter
func (b *Bloom) IsLogInBloom(log *Log) bool {
	hasher := keccak.DefaultKeccakPool.Get()

	// Check if the log address is present
	addressPresent := b.isByteArrPresent(hasher, log.Address.Bytes())
	if !addressPresent {
		return false
	}

	// Check if all the topics are present
	for _, topic := range log.Topics {
		topicsPresent := b.isByteArrPresent(hasher, topic.Bytes())

		if !topicsPresent {
			return false
		}
	}

	keccak.DefaultKeccakPool.Put(hasher)

	return true
}

// isByteArrPresent checks if the byte array is possibly present in the Bloom filter
func (b *Bloom) isByteArrPresent(hasher *keccak.Keccak, data []byte) bool {
	hasher.Reset()
	hasher.Write(data[:])
	buf := hasher.Read()

	for i := 0; i < 6; i += 2 {
		// Find the global bit location
		bit := (uint(buf[i+1]) + (uint(buf[i]) << 8)) & 2047

		// Find where the bit maps in the [0..255] byte array
		byteLocation := 256 - 1 - bit/8
		bitLocation := bit % 8

		referenceByte := b[byteLocation]

		isSet := int(referenceByte & (1 << (bitLocation - 1)))

		if isSet == 0 {
			return false
		}
	}

	return true
}

func (r Receipts) MarshalRLPTo(dst []byte) []byte {
	return MarshalRLPTo(r.MarshalRLPWith, dst)
}

func (r *Receipts) MarshalRLPWith(a *fastrlp.Arena) *fastrlp.Value {
	vv := a.NewArray()
	for _, rr := range *r {
		vv.Set(rr.MarshalRLPWith(a))
	}

	return vv
}

func (r *Receipt) MarshalRLP() []byte {
	return r.MarshalRLPTo(nil)
}

func (r *Receipt) MarshalRLPTo(dst []byte) []byte {
	return MarshalRLPTo(r.MarshalRLPWith, dst)
}

// MarshalRLPWith marshals a receipt with a specific fastrlp.Arena
func (r *Receipt) MarshalRLPWith(a *fastrlp.Arena) *fastrlp.Value {
	vv := a.NewArray()
	if r.Status != nil {
		vv.Set(a.NewUint(uint64(*r.Status)))
	} else {
		vv.Set(a.NewBytes(r.Root[:]))
	}

	vv.Set(a.NewUint(r.CumulativeGasUsed))
	vv.Set(a.NewCopyBytes(r.LogsBloom[:]))
	vv.Set(r.MarshalLogsWith(a))

	return vv
}

// MarshalLogsWith marshals the logs of the receipt to RLP with a specific fastrlp.Arena
func (r *Receipt) MarshalLogsWith(a *fastrlp.Arena) *fastrlp.Value {
	if len(r.Logs) == 0 {
		// There are no receipts, write the RLP null array entry
		return a.NewNullArray()
	}

	logs := a.NewArray()

	for _, l := range r.Logs {
		logs.Set(l.MarshalRLPWith(a))
	}

	return logs
}

func (r *Receipts) UnmarshalRLP(input []byte) error {
	return UnmarshalRlp(r.UnmarshalRLPFrom, input)
}

func (r *Receipts) UnmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	elems, err := v.GetElems()
	if err != nil {
		return err
	}

	for _, elem := range elems {
		rr := &Receipt{}
		if err := rr.UnmarshalRLPFrom(p, elem); err != nil {
			return err
		}

		(*r) = append(*r, rr)
	}

	return nil
}

func (r *Receipt) UnmarshalRLP(input []byte) error {
	return UnmarshalRlp(r.UnmarshalRLPFrom, input)
}

// UnmarshalRLP unmarshals a Receipt in RLP format
func (r *Receipt) UnmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	elems, err := v.GetElems()
	if err != nil {
		return err
	}

	if len(elems) < 4 {
		return fmt.Errorf("incorrect number of elements to decode receipt, expected at least 4 but found %d",
			len(elems))
	}

	// root or status
	buf, err := elems[0].Bytes()
	if err != nil {
		return err
	}

	switch size := len(buf); size {
	case 32:
		// root
		copy(r.Root[:], buf[:])
	case 1:
		// status
		r.SetStatus(ReceiptStatus(buf[0]))
	default:
		r.SetStatus(0)
	}

	// cumulativeGasUsed
	if r.CumulativeGasUsed, err = elems[1].GetUint64(); err != nil {
		return err
	}
	// logsBloom
	if _, err = elems[2].GetBytes(r.LogsBloom[:0], 256); err != nil {
		return err
	}

	// logs
	logsElems, err := v.Get(3).GetElems()
	if err != nil {
		return err
	}

	for _, elem := range logsElems {
		log := &Log{}
		if err := log.UnmarshalRLPFrom(p, elem); err != nil {
			return err
		}

		r.Logs = append(r.Logs, log)
	}

	return nil
}

func (l *Log) UnmarshalRLPFrom(p *fastrlp.Parser, v *fastrlp.Value) error {
	elems, err := v.GetElems()
	if err != nil {
		return err
	}

	if len(elems) < 3 {
		return fmt.Errorf("incorrect number of elements to decode log, expected at least 3 but found %d",
			len(elems))
	}

	// address
	if err := elems[0].GetAddr(l.Address[:]); err != nil {
		return err
	}
	// topics
	topicElems, err := elems[1].GetElems()
	if err != nil {
		return err
	}

	l.Topics = make([]Hash, len(topicElems))

	for indx, topic := range topicElems {
		if err := topic.GetHash(l.Topics[indx][:]); err != nil {
			return err
		}
	}

	// data
	if l.Data, err = elems[2].GetBytes(l.Data[:0]); err != nil {
		return err
	}

	return nil
}
