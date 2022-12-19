package rpc

import (
	"fmt"

	"github.com/sunvim/dogesyncer/types"
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

// not support "earliest" and "pending"
func (s *RpcServer) GetBalance(method string, params ...any) any {
	var gp *GetBalanceParams
	err := gp.Unmarshal(params...)
	if err != nil {
		return err
	}
	if *gp.Number == PendingBlockNumber || *gp.Number == EarliestBlockNumber {
		return fmt.Errorf("not support pending and earliest block query")
	}

	return nil
}
