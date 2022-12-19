package rpc

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/sunvim/dogesyncer/types"
)

type RpcFunc func(method string, params ...any) any

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
