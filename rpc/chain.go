package rpc

import (
	"strconv"
	"strings"
)

func (s *RpcServer) GetBlockNumber(method string, params ...any) any {
	num := strconv.FormatInt(int64(s.blockchain.Header().Number), 16)
	return strings.Join([]string{"0x", num}, "")
}
