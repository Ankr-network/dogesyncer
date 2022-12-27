package types

var (
	HeaderHash func(h *Header) Hash
)

func init() {
	HeaderHash = defHeaderHash
}

func defHeaderHash(h *Header) (hash Hash) {
	msg, _ := CalculateHeaderHash(h)
	return BytesToHash(msg)
}

// ComputeHash computes the hash of the header
func (h *Header) ComputeHash() *Header {
	h.Hash = HeaderHash(h)
	return h
}
