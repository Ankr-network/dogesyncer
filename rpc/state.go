package rpc

import (
	"fmt"
	"github.com/ankr/dogesyncer/blockchain"
	"github.com/ankr/dogesyncer/state"
	"github.com/ankr/dogesyncer/types"
	"math/big"
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
func (s *RpcServer) GetBalance(method string, params ...any) any {
	var gp *GetBalanceParams
	err := gp.Unmarshal(params...)
	if err != nil {
		return err
	}
	if *gp.Number == PendingBlockNumber || *gp.Number == EarliestBlockNumber {
		return fmt.Errorf("not support pending and earliest block query")
	}

	header, found := s.blockchain.GetHeaderByNumber(uint64(*gp.Number))
	if !found {
		return blockchain.ErrStateNotFound
	}

	account, err := s.blockchain.GetAccount(header.StateRoot, gp.Address)
	if err != nil {
		return err
	}
	return account.Balance.String()
}

func (s *RpcServer) Call(method string, params ...any) any {
	var (
		header *types.Header
		err    error
	)

	var gp *GetBalanceParams
	err = gp.Unmarshal(params...)
	if err != nil {
		return err
	}
	if *gp.Number == PendingBlockNumber || *gp.Number == EarliestBlockNumber {
		return fmt.Errorf("not support pending and earliest block query")
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
		return err
	}

	trans, err := decodeTxn(tx, *transition)

	result, err := transition.Apply(trans)
	if err != nil {
		return err
	}

	// Check if an EVM revert happened
	if result.Reverted() {
		return fmt.Errorf("%w: reverted", result.Err)

	}

	if result.Failed() {
		return fmt.Errorf("unable to execute call: %w", result.Err)
	}

	return argBytesPtr(result.ReturnValue)
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
