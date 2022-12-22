package rpc

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/sunvim/dogesyncer/types"
)

type RpcFunc func(method string, params ...any) (any, Error)

type Request struct {
	Version string `json:"jsonrpc,omitempty"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      any    `json:"id"`
}

type Response struct {
	ID      any    `json:"id"`
	Version string `json:"jsonrpc"`
	Result  any    `json:"result"`
}

type ErrorResponse struct {
	ID      any    `json:"id"`
	Version string `json:"jsonrpc"`
	Error   any    `json:"error"`
}

var (
	reqPool = &sync.Pool{
		New: func() any {
			return &Request{}
		},
	}

	resPool = &sync.Pool{
		New: func() any {
			return &Response{}
		},
	}

	resErrorPool = &sync.Pool{
		New: func() any {
			return &ErrorResponse{}
		},
	}
)

const (
	PendingBlockFlag  = "pending"
	LatestBlockFlag   = "latest"
	EarliestBlockFlag = "earliest"
)

const (
	PendingBlockNumber  = BlockNumber(-3)
	LatestBlockNumber   = BlockNumber(-2)
	EarliestBlockNumber = BlockNumber(-1)
)

type BlockNumber int64

type BlockNumberOrHash struct {
	BlockNumber *BlockNumber `json:"blockNumber,omitempty"`
	BlockHash   *types.Hash  `json:"blockHash,omitempty"`
}

// UnmarshalJSON will try to extract the filter's data.
// Here are the possible input formats :
//
// 1 - "latest", "pending" or "earliest"	- self-explaining keywords
// 2 - "0x2"								- block number #2 (EIP-1898 backward compatible)
// 3 - {blockNumber:	"0x2"}				- EIP-1898 compliant block number #2
// 4 - {blockHash:		"0xe0e..."}			- EIP-1898 compliant block hash 0xe0e...
func (bnh *BlockNumberOrHash) UnmarshalJSON(data []byte) error {
	type bnhCopy BlockNumberOrHash

	var placeholder bnhCopy

	err := json.Unmarshal(data, &placeholder)
	if err != nil {
		number, err := StringToBlockNumber(string(data))
		if err != nil {
			return err
		}

		placeholder.BlockNumber = &number
	}

	// Try to extract object
	bnh.BlockNumber = placeholder.BlockNumber
	bnh.BlockHash = placeholder.BlockHash

	if bnh.BlockNumber != nil && bnh.BlockHash != nil {
		return fmt.Errorf("cannot use both block number and block hash as filters")
	} else if bnh.BlockNumber == nil && bnh.BlockHash == nil {
		return fmt.Errorf("block number and block hash are empty, please provide one of them")
	}

	return nil
}

func StringToBlockNumber(str string) (BlockNumber, error) {
	if str == "" {
		return 0, fmt.Errorf("value is empty")
	}

	str = strings.Trim(str, "\"")
	switch str {
	case PendingBlockFlag:
		return PendingBlockNumber, nil
	case LatestBlockFlag:
		return LatestBlockNumber, nil
	case EarliestBlockFlag:
		return EarliestBlockNumber, nil
	}

	n, err := types.ParseUint64orHex(&str)
	if err != nil {
		return 0, err
	}

	return BlockNumber(n), nil
}

func CreateBlockNumberPointer(str string) (*BlockNumber, error) {
	blockNumber, err := StringToBlockNumber(str)
	if err != nil {
		return nil, err
	}

	return &blockNumber, nil
}

// UnmarshalJSON automatically decodes the user input for the block number, when a JSON RPC method is called
func (b *BlockNumber) UnmarshalJSON(buffer []byte) error {
	num, err := StringToBlockNumber(string(buffer))
	if err != nil {
		return err
	}

	*b = num

	return nil
}

type progression struct {
	Type          string `json:"type"`
	SyncingPeer   string `json:"syncingPeer"`
	StartingBlock string `json:"startingBlock"`
	CurrentBlock  string `json:"currentBlock"`
	HighestBlock  string `json:"highestBlock"`
}

type argUint64 uint64
type argBytes []byte
type argBig big.Int

func argUintPtr(n uint64) *argUint64 {
	v := argUint64(n)

	return &v
}

// Redefine to implement getHash() of transactionOrHash
type transactionHash types.Hash

func (h transactionHash) getHash() types.Hash { return types.Hash(h) }

type jsonHeader struct {
	ParentHash   types.Hash    `json:"parentHash"`
	Sha3Uncles   types.Hash    `json:"sha3Uncles"`
	Miner        types.Address `json:"miner"`
	StateRoot    types.Hash    `json:"stateRoot"`
	TxRoot       types.Hash    `json:"transactionsRoot"`
	ReceiptsRoot types.Hash    `json:"receiptsRoot"`
	LogsBloom    types.Bloom   `json:"logsBloom"`
	Difficulty   argUint64     `json:"difficulty"`
	Number       argUint64     `json:"number"`
	GasLimit     argUint64     `json:"gasLimit"`
	GasUsed      argUint64     `json:"gasUsed"`
	Timestamp    argUint64     `json:"timestamp"`
	ExtraData    argBytes      `json:"extraData"`
	MixHash      types.Hash    `json:"mixHash"`
	Nonce        types.Nonce   `json:"nonce"`
	Hash         types.Hash    `json:"hash"`
}

type transaction struct {
	Nonce       argUint64      `json:"nonce"`
	GasPrice    argBig         `json:"gasPrice"`
	Gas         argUint64      `json:"gas"`
	To          *types.Address `json:"to"`
	Value       argBig         `json:"value"`
	Input       argBytes       `json:"input"`
	V           argBig         `json:"v"`
	R           argBig         `json:"r"`
	S           argBig         `json:"s"`
	Hash        types.Hash     `json:"hash"`
	From        types.Address  `json:"from"`
	BlockHash   *types.Hash    `json:"blockHash"`
	BlockNumber *argUint64     `json:"blockNumber"`
	TxIndex     *argUint64     `json:"transactionIndex"`
}

// For union type of transaction and types.Hash
type transactionOrHash interface {
	getHash() types.Hash
}

type block struct {
	jsonHeader
	TotalDifficulty argUint64           `json:"totalDifficulty"`
	Size            argUint64           `json:"size"`
	Transactions    []transactionOrHash `json:"transactions"`
	Uncles          []types.Hash        `json:"uncles"`
}

func toBlock(b *types.Block, fullTx bool) *block {
	h := b.Header
	jh := toJSONHeader(h)

	res := &block{
		jsonHeader:      *jh,
		TotalDifficulty: argUint64(h.Difficulty), // not needed for POS
		Size:            argUint64(b.Size()),
		Transactions:    []transactionOrHash{},
		Uncles:          []types.Hash{},
	}

	for idx, txn := range b.Transactions {
		if fullTx {
			res.Transactions = append(
				res.Transactions,
				toTransaction(
					txn,
					argUintPtr(b.Number()),
					argHashPtr(b.Hash()),
					&idx,
				),
			)
		} else {
			res.Transactions = append(
				res.Transactions,
				transactionHash(txn.Hash()),
			)
		}
	}

	for _, uncle := range b.Uncles {
		res.Uncles = append(res.Uncles, uncle.Hash)
	}

	return res
}

func toJSONHeader(h *types.Header) *jsonHeader {
	return &jsonHeader{
		ParentHash:   h.ParentHash,
		Sha3Uncles:   h.Sha3Uncles,
		Miner:        h.Miner,
		StateRoot:    h.StateRoot,
		TxRoot:       h.TxRoot,
		ReceiptsRoot: h.ReceiptsRoot,
		LogsBloom:    h.LogsBloom,
		Difficulty:   argUint64(h.Difficulty),
		Number:       argUint64(h.Number),
		GasLimit:     argUint64(h.GasLimit),
		GasUsed:      argUint64(h.GasUsed),
		Timestamp:    argUint64(h.Timestamp),
		ExtraData:    argBytes(h.ExtraData),
		MixHash:      h.MixHash,
		Nonce:        h.Nonce,
		Hash:         h.Hash,
	}
}

func toTransaction(
	t *types.Transaction,
	blockNumber *argUint64,
	blockHash *types.Hash,
	txIndex *int,
) *transaction {
	res := &transaction{
		Nonce:    argUint64(t.Nonce),
		GasPrice: argBig(*t.GasPrice),
		Gas:      argUint64(t.Gas),
		To:       t.To,
		Value:    argBig(*t.Value),
		Input:    t.Input,
		V:        argBig(*t.V),
		R:        argBig(*t.R),
		S:        argBig(*t.S),
		Hash:     t.Hash(),
		From:     t.From,
	}

	if blockNumber != nil {
		res.BlockNumber = blockNumber
	}

	if blockHash != nil {
		res.BlockHash = blockHash
	}

	if txIndex != nil {
		res.TxIndex = argUintPtr(uint64(*txIndex))
	}

	return res
}

func (t transaction) getHash() types.Hash { return t.Hash }

func argHashPtr(h types.Hash) *types.Hash {
	return &h
}

type receipt struct {
	Root              types.Hash     `json:"root"`
	CumulativeGasUsed argUint64      `json:"cumulativeGasUsed"`
	LogsBloom         types.Bloom    `json:"logsBloom"`
	Logs              []*Log         `json:"logs"`
	Status            argUint64      `json:"status"`
	TxHash            types.Hash     `json:"transactionHash"`
	TxIndex           argUint64      `json:"transactionIndex"`
	BlockHash         types.Hash     `json:"blockHash"`
	BlockNumber       argUint64      `json:"blockNumber"`
	GasUsed           argUint64      `json:"gasUsed"`
	ContractAddress   *types.Address `json:"contractAddress"`
	FromAddr          types.Address  `json:"from"`
	ToAddr            *types.Address `json:"to"`
}

type Log struct {
	Address     types.Address `json:"address"`
	Topics      []types.Hash  `json:"topics"`
	Data        argBytes      `json:"data"`
	BlockNumber argUint64     `json:"blockNumber"`
	TxHash      types.Hash    `json:"transactionHash"`
	TxIndex     argUint64     `json:"transactionIndex"`
	BlockHash   types.Hash    `json:"blockHash"`
	LogIndex    argUint64     `json:"logIndex"`
	Removed     bool          `json:"removed"`
}
