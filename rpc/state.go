package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ankr/dogesyncer/blockchain"
	"github.com/ankr/dogesyncer/helper/hex"
	"github.com/ankr/dogesyncer/state"
	"github.com/ankr/dogesyncer/state/runtime"
	"github.com/ankr/dogesyncer/types"
	"github.com/dogechain-lab/fastrlp"
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
	adders, ok := params[0].(string)
	if !ok {
		return fmt.Errorf("error param wallet address")
	}
	gp.Address = types.StringToAddress(adders)

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
		return nil, NewInvalidParamsError(state.ErrStateNotFound.Error())
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
		return nil, NewInvalidParamsError(state.ErrStateNotFound.Error())
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
		return nil, NewInvalidParamsError(state.ErrStateNotFound.Error())
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
		return nil, NewInvalidParamsError(state.ErrStateNotFound.Error())
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
		return nil, NewInvalidParamsError(blockchain.ErrNoBlockHeader.Error())
	}

	// deserialization
	tx := new(TxnArgs)
	params0, err := json.Marshal(paramsIn[0])
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if err := json.Unmarshal(params0, tx); err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	transition, err := s.executor.BeginTxn(header.StateRoot, header, header.Miner)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	trans, err := decodeTxn(tx, *transition)

	// If the caller didn't supply the gas limit in the message, then we set it to maximum possible => block gas limit
	if trans.Gas == 0 {
		trans.Gas = header.GasLimit
	}

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

func (s *RpcServer) EstimateGas(method string, params ...any) (any, Error) {
	var (
		header *types.Header
		err    error
	)

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
		return nil, NewInvalidParamsError(blockchain.ErrNoBlockHeader.Error())
	}

	forksInTime := s.executor.GetForksInTime(blockNum)

	// deserialization
	tx := new(TxnArgs)
	params0, err := json.Marshal(paramsIn[0])
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}
	if err := json.Unmarshal(params0, tx); err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	transition, err := s.executor.BeginTxn(header.StateRoot, header, header.Miner)
	if err != nil {
		return nil, NewInvalidParamsError(err.Error())
	}

	trans, err := decodeTxn(tx, *transition)

	var standardGas uint64
	if trans.IsContractCreation() && forksInTime.Homestead {
		standardGas = state.TxGasContractCreation
	} else {
		standardGas = state.TxGas
	}

	var (
		lowEnd  = standardGas
		highEnd uint64
	)

	// If the gas limit was passed in, use it as a ceiling
	if trans.Gas != 0 && trans.Gas >= standardGas {
		highEnd = trans.Gas
	} else {
		// If not, use the referenced block number
		highEnd = header.GasLimit
	}

	gasPriceInt := new(big.Int).Set(trans.GasPrice)
	valueInt := new(big.Int).Set(trans.Value)

	var availableBalance *big.Int

	// If the sender address is present, figure out how much available funds
	// are we working with
	if trans.From != types.ZeroAddress {
		// Get the account balance
		// If the account is not initialized yet in state,
		// assume it's an empty account
		accountBalance := big.NewInt(0)
		acc, err := s.blockchain.GetAccount(header.StateRoot, trans.From)

		if err != nil && !errors.Is(err, ErrStateNotFound) {
			// An unrelated error occurred, return it
			return nil, NewInvalidParamsError(err.Error())
		} else if err == nil {
			// No error when fetching the account,
			// read the balance from state
			accountBalance = acc.Balance
		}

		availableBalance = new(big.Int).Set(accountBalance)

		if trans.Value != nil {
			if valueInt.Cmp(availableBalance) > 0 {
				return 0, NewInvalidParamsError(state.ErrInsufficientFunds.Error())
			}

			availableBalance.Sub(availableBalance, valueInt)
		}
	}

	// Recalculate the gas ceiling based on the available funds (if any)
	// and the passed in gas price (if present)
	if gasPriceInt.BitLen() != 0 && // Gas price has been set
		availableBalance != nil && // Available balance is found
		availableBalance.Cmp(big.NewInt(0)) > 0 { // Available balance > 0
		gasAllowance := new(big.Int).Div(availableBalance, gasPriceInt)

		// Check the gas allowance for this account, make sure high end is capped to it
		if gasAllowance.IsUint64() && highEnd > gasAllowance.Uint64() {
			s.logger.Debug(
				fmt.Sprintf(
					"Gas estimation high-end capped by allowance [%d]",
					gasAllowance.Uint64(),
				),
			)

			highEnd = gasAllowance.Uint64()
		}
	}

	// Checks if executor level valid gas errors occurred
	isGasApplyError := func(err error) bool {
		return errors.Is(err, state.ErrNotEnoughIntrinsicGas)
	}

	// Checks if EVM level valid gas errors occurred
	isGasEVMError := func(err error) bool {
		return errors.Is(err, runtime.ErrOutOfGas) ||
			errors.Is(err, runtime.ErrCodeStoreOutOfGas)
	}

	// Checks if the EVM reverted during execution
	isEVMRevertError := func(err error) bool {
		return errors.Is(err, runtime.ErrExecutionReverted)
	}

	// Run the transaction with the specified gas value.
	// Returns a status indicating if the transaction failed and the accompanying error
	testTransaction := func(gas uint64, shouldOmitErr bool) (bool, error) {
		// Create a dummy transaction with the new gas
		txn := trans.Copy()
		txn.Gas = gas

		result, applyErr := transition.Apply(trans)

		if applyErr != nil {
			// Check the application error.
			// Gas apply errors are valid, and should be ignored
			if isGasApplyError(applyErr) && shouldOmitErr {
				// Specifying the transaction failed, but not providing an error
				// is an indication that a valid error occurred due to low gas,
				// which will increase the lower bound for the search
				return true, nil
			}

			return true, applyErr
		}

		// Check if an out of gas error happened during EVM execution
		if result.Failed() {
			if isGasEVMError(result.Err) && shouldOmitErr {
				// Specifying the transaction failed, but not providing an error
				// is an indication that a valid error occurred due to low gas,
				// which will increase the lower bound for the search
				return true, nil
			}

			if isEVMRevertError(result.Err) {
				// The EVM reverted during execution, attempt to extract the
				// error message and return it
				return true, NewInvalidRequestError(fmt.Sprintf("%v: reverted", result.Err))
			}

			return true, result.Err
		}

		return false, nil
	}

	// Start the binary search for the lowest possible gas price
	for lowEnd < highEnd {
		mid := (lowEnd + highEnd) / 2

		failed, testErr := testTransaction(mid, true)
		if testErr != nil &&
			!isEVMRevertError(testErr) {
			// Reverts are ignored in the binary search, but are checked later on
			// during the execution for the optimal gas limit found
			return 0, NewInvalidRequestError(testErr.Error())
		}

		if failed {
			// If the transaction failed => increase the gas
			lowEnd = mid + 1
		} else {
			// If the transaction didn't fail => make this ok value the high end
			highEnd = mid
		}
	}

	// Check if the highEnd is a good value to make the transaction pass
	failed, err := testTransaction(highEnd, false)
	if failed {
		// The transaction shouldn't fail, for whatever reason, at highEnd
		return 0, NewInvalidRequestError(fmt.Errorf(
			"unable to apply transaction even for the highest gas limit %d: %w",
			highEnd,
			err,
		).Error())
	}

	return hex.EncodeUint64(highEnd), nil
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
