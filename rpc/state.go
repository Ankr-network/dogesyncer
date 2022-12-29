package rpc

import (
	"fmt"
	"math/big"

	"github.com/ankr/dogesyncer/state"
	itrie "github.com/ankr/dogesyncer/state/immutable-trie"
	"github.com/ankr/dogesyncer/types"
	"github.com/dogechain-lab/fastrlp"
)

type GetBalanceParams struct {
	Address types.Address
	Number  *BlockNumber
}

func (gp *GetBalanceParams) Unmarshal(params ...any) error {
	if len(params) != 2 {
		return fmt.Errorf("error params")
	}
	addrs, ok := params[0].(string)
	if !ok {
		return fmt.Errorf("error param wallet address")
	}
	gp.Address = types.StringToAddress(addrs)

	nums, ok := params[1].(string)
	if !ok {
		return fmt.Errorf("error param block number")
	}
	var err error
	gp.Number, err = CreateBlockNumberPointer(nums)
	if err != nil {
		return err
	}
	return nil
}

// GetBalance not support "earliest" and "pending"
func (s *RpcServer) GetBalance(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if len(paramsIn) != 2 {
		return nil, NewInvalidParamsError(fmt.Sprintf("missing value for required argument %d", len(paramsIn)))
	}
	blockNum, numErr := GetNumericBlockNumber(paramsIn[1].(string), s.blockchain)
	if numErr != nil {
		return nil, NewInvalidParamsError(numErr.Error())
	}

	header, found := s.blockchain.GetHeaderByNumber(blockNum)
	if !found {
		return nil, NewInvalidParamsError(itrie.ErrStateNotFound.Error())
	}

	account, err := s.blockchain.GetAccount(header.StateRoot, types.StringToAddress(paramsIn[0].(string)))
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	return types.EncodeBigInt(account.Balance), nil
}

func (s *RpcServer) GetCode(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if len(paramsIn) != 2 {
		return nil, NewInvalidParamsError(fmt.Sprintf("missing value for required argument %d", len(paramsIn)))
	}
	blockNum, numErr := GetNumericBlockNumber(paramsIn[1].(string), s.blockchain)
	if numErr != nil {
		return nil, NewInvalidParamsError(numErr.Error())
	}

	header, found := s.blockchain.GetHeaderByNumber(blockNum)
	if !found {
		return nil, NewInvalidParamsError(itrie.ErrStateNotFound.Error())
	}

	account, err := s.blockchain.GetAccount(header.StateRoot, types.StringToAddress(paramsIn[0].(string)))
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	code, err := s.blockchain.GetCode(types.BytesToHash(account.CodeHash))
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	return types.EncodeBytes(code), nil
}

func (s *RpcServer) GetStorageAt(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if len(paramsIn) != 3 {
		return nil, NewInvalidParamsError(fmt.Sprintf("missing value for required argument %d", len(paramsIn)))
	}
	blockNum, numErr := GetNumericBlockNumber(paramsIn[2].(string), s.blockchain)
	if numErr != nil {
		return nil, NewInvalidParamsError(numErr.Error())
	}

	header, found := s.blockchain.GetHeaderByNumber(blockNum)
	if !found {
		return nil, NewInvalidParamsError(itrie.ErrStateNotFound.Error())
	}

	storage, err := s.blockchain.GetStorage(header.StateRoot, types.StringToAddress(paramsIn[0].(string)), types.StringToHash(paramsIn[1].(string)))
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	// Parse the RLP value
	p := &fastrlp.Parser{}

	v, err := p.Parse(storage)
	if err != nil {
		return argBytesPtr(types.ZeroHash[:]), nil
	}

	data, err := v.Bytes()

	if err != nil {
		return argBytesPtr(types.ZeroHash[:]), nil
	}

	// Pad to return 32 bytes data
	return types.EncodeBytes(types.BytesToHash(data).Bytes()), nil
}

func (s *RpcServer) GetTransactionCount(method string, params ...any) (any, Error) {
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if len(paramsIn) != 2 {
		return nil, NewInvalidParamsError(fmt.Sprintf("missing value for required argument %d", len(paramsIn)))
	}
	blockNum, numErr := GetNumericBlockNumber(paramsIn[1].(string), s.blockchain)
	if numErr != nil {
		return nil, NewInvalidParamsError(numErr.Error())
	}

	header, found := s.blockchain.GetHeaderByNumber(blockNum)
	if !found {
		return nil, NewInvalidParamsError(itrie.ErrStateNotFound.Error())
	}

	account, err := s.blockchain.GetAccount(header.StateRoot, types.StringToAddress(paramsIn[0].(string)))
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	return types.EncodeUint64(account.Nonce), nil
}

func (s *RpcServer) Call(method string, params ...any) (any, Error) {
	var (
		header *types.Header
		err    error
	)

	var gp *GetBalanceParams
	err = gp.Unmarshal(params...)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if *gp.Number == PendingBlockNumber || *gp.Number == EarliestBlockNumber {
		return nil, NewInvalidParamsError("not support pending and earliest block query")
	}

	header, found := s.blockchain.GetHeaderByNumber(uint64(*gp.Number))
	if !found {

	}

	// todo deserialization
	var tx *TxnArgs

	// If the caller didn't supply the gas limit in the message, then we set it to maximum possible => block gas limit
	if *tx.Gas == 0 {
		*tx.Gas = argUint64(header.GasLimit)
	}

	transition, err := s.executor.BeginTxn(header.StateRoot, header, header.Miner)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	trans, err := decodeTxn(tx, *transition)

	result, err := transition.Apply(trans)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	// Check if an EVM revert happened
	if result.Reverted() {
		return nil, NewInvalidRequestError(fmt.Sprintf("%v: reverted", result.Err))

	}

	if result.Failed() {
		return nil, NewInvalidRequestError(fmt.Sprintf("unable to execute call: %v", result.Err))
	}

	return argBytesPtr(result.ReturnValue), nil
}

type TxnArgs struct {
	From     *types.Address
	To       *types.Address
	Gas      *argUint64
	GasPrice *argBytes
	Value    *argBytes
	Data     *argBytes
	Input    *argBytes
	Nonce    *argUint64
}

func decodeTxn(arg *TxnArgs, txExecutor state.Transition) (*types.Transaction, error) {
	// set default values
	if arg.From == nil {
		arg.From = &types.ZeroAddress
		arg.Nonce = argUintPtr(0)
	} else if arg.Nonce == nil {
		// get nonce from the pool
		nonce := txExecutor.GetNonce(*arg.From)
		arg.Nonce = argUintPtr(nonce)
	}

	if arg.Value == nil {
		arg.Value = argBytesPtr([]byte{})
	}

	if arg.GasPrice == nil {
		arg.GasPrice = argBytesPtr([]byte{})
	}

	var input []byte
	if arg.Data != nil {
		input = *arg.Data
	} else if arg.Input != nil {
		input = *arg.Input
	}

	if arg.To == nil {
		if input == nil {
			return nil, fmt.Errorf("contract creation without data provided")
		}
	}

	if input == nil {
		input = []byte{}
	}

	if arg.Gas == nil {
		arg.Gas = argUintPtr(0)
	}

	txn := &types.Transaction{
		From:     *arg.From,
		Gas:      uint64(*arg.Gas),
		GasPrice: new(big.Int).SetBytes(*arg.GasPrice),
		Value:    new(big.Int).SetBytes(*arg.Value),
		Input:    input,
		Nonce:    uint64(*arg.Nonce),
	}
	if arg.To != nil {
		txn.To = arg.To
	}

	txn.Hash()

	return txn, nil
}
