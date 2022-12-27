package blockchain

import (
	"github.com/ankr/dogesyncer/crypto"
	"github.com/ankr/dogesyncer/types"
)

func ecrecoverFromHeader(h *types.Header) (types.Address, error) {
	// get the extra part that contains the seal
	extra, err := types.GetIbftExtra(h)
	if err != nil {
		return types.Address{}, err
	}
	// get the sig
	msg, err := types.CalculateHeaderHash(h)
	if err != nil {
		return types.Address{}, err
	}

	pub, err := crypto.RecoverPubkey(extra.Seal, crypto.Keccak256(msg))
	if err != nil {
		return types.Address{}, err
	}

	return crypto.PubKeyToAddress(pub), nil
}
