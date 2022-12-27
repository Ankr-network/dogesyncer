package blockchain

import "errors"

var (
	ErrNoBlock              = errors.New("no block data passed in")
	ErrNoBlockHeader        = errors.New("no block header data passed in")
	ErrParentNotFound       = errors.New("parent block not found")
	ErrInvalidParentHash    = errors.New("parent block hash is invalid")
	ErrParentHashMismatch   = errors.New("invalid parent block hash")
	ErrInvalidBlockSequence = errors.New("invalid block sequence")
	ErrInvalidSha3Uncles    = errors.New("invalid block sha3 uncles root")
	ErrInvalidTxRoot        = errors.New("invalid block transactions root")
	ErrInvalidReceiptsSize  = errors.New("invalid number of receipts")
	ErrInvalidStateRoot     = errors.New("invalid block state root")
	ErrInvalidGasUsed       = errors.New("invalid block gas used")
	ErrInvalidReceiptsRoot  = errors.New("invalid block receipts root")
	ErrNilStorageBuilder    = errors.New("nil storage builder")
	ErrClosed               = errors.New("blockchain is closed")
	ErrExistBlock           = errors.New("exist block")
)
