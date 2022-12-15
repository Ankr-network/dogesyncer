package rpc

import (
	"fmt"

	"github.com/sunvim/dogesyncer/helper/hex"
	"github.com/sunvim/dogesyncer/helper/keccak"
)

// Web3 is the web3 jsonrpc endpoint
type Web3 struct {
	chainID uint64
}

var _clientVersionTemplate = "dogechain [chain-id: %d] [version: %s]"

// ClientVersion returns the version of the web3 client (web3_clientVersion)
func (w *Web3) ClientVersion() (interface{}, error) {
	return fmt.Sprintf(
		_clientVersionTemplate,
		w.chainID,
		"for rpc",
	), nil
}

// Sha3 returns Keccak-256 (not the standardized SHA3-256) of the given data
func (w *Web3) Sha3(val string) (interface{}, error) {
	v, err := hex.DecodeHex(val)
	if err != nil {
		return nil, NewInvalidRequestError("Invalid hex string")
	}

	dst := keccak.Keccak256(nil, v)

	return hex.EncodeToHex(dst), nil
}

func (s *RpcServer) Web3ClientVersion(method string, params ...any) (any, Error) {
	res, err := s.endpoints.Web3.ClientVersion()
	if err != nil {
		return nil, NewInternalError(err.Error())
	}
	return res, nil
}

func (s *RpcServer) Web3Sha3(method string, params ...any) (any, Error) {
	fmt.Println("params", params)
	paramsIn, err := GetPrams(params...)
	if err != nil {
		return nil, err
	}
	res, errWbe3 := s.endpoints.Web3.Sha3(paramsIn[0])
	if errWbe3 != nil {
		return nil, NewInvalidRequestError(errWbe3.Error())
	}
	return res, nil
}
