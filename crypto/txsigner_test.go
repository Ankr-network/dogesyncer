package crypto

import (
	"github.com/ankr/dogesyncer/helper/hex"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
	"testing"

	"github.com/ankr/dogesyncer/types"
	"github.com/stretchr/testify/assert"
)

func TestFrontierSigner(t *testing.T) {
	signer := &FrontierSigner{}

	toAddress := types.StringToAddress("1")
	key, err := GenerateKey()
	assert.NoError(t, err)

	txn := &types.Transaction{
		To:       &toAddress,
		Value:    big.NewInt(10),
		GasPrice: big.NewInt(0),
	}
	signedTx, err := signer.SignTx(txn, key)
	assert.NoError(t, err)

	from, err := signer.Sender(signedTx)
	assert.NoError(t, err)
	assert.Equal(t, from, PubKeyToAddress(&key.PublicKey))
}

func TestEIP155Signer_Sender(t *testing.T) {
	toAddress := types.StringToAddress("1")

	testTable := []struct {
		name    string
		chainID *big.Int
	}{
		{
			"mainnet",
			big.NewInt(1),
		},
		{
			"expanse mainnet",
			big.NewInt(2),
		},
		{
			"ropsten",
			big.NewInt(3),
		},
		{
			"rinkeby",
			big.NewInt(4),
		},
		{
			"goerli",
			big.NewInt(5),
		},
		{
			"kovan",
			big.NewInt(42),
		},
		{
			"geth private",
			big.NewInt(1337),
		},
		{
			"mega large",
			big.NewInt(0).Exp(big.NewInt(2), big.NewInt(20), nil), // 2**20
		},
	}

	for _, testCase := range testTable {
		t.Run(testCase.name, func(t *testing.T) {
			key, keyGenError := GenerateKey()
			if keyGenError != nil {
				t.Fatalf("Unable to generate key")
			}

			txn := &types.Transaction{
				To:       &toAddress,
				Value:    big.NewInt(1),
				GasPrice: big.NewInt(0),
			}

			signer := NewEIP155Signer(testCase.chainID.Uint64())

			signedTx, signErr := signer.SignTx(txn, key)
			if signErr != nil {
				t.Fatalf("Unable to sign transaction")
			}

			recoveredSender, recoverErr := signer.Sender(signedTx)
			if recoverErr != nil {
				t.Fatalf("Unable to recover sender")
			}

			assert.Equal(t, recoveredSender.String(), PubKeyToAddress(&key.PublicKey).String())
		})
	}
}

func TestEIP155Signer_ChainIDMismatch(t *testing.T) {
	chainIDS := []uint64{1, 10, 100}
	toAddress := types.StringToAddress("1")

	for _, chainIDTop := range chainIDS {
		key, keyGenError := GenerateKey()
		if keyGenError != nil {
			t.Fatalf("Unable to generate key")
		}

		txn := &types.Transaction{
			To:       &toAddress,
			Value:    big.NewInt(1),
			GasPrice: big.NewInt(0),
		}

		signer := NewEIP155Signer(chainIDTop)

		signedTx, signErr := signer.SignTx(txn, key)
		if signErr != nil {
			t.Fatalf("Unable to sign transaction")
		}

		for _, chainIDBottom := range chainIDS {
			signerBottom := NewEIP155Signer(chainIDBottom)

			recoveredSender, recoverErr := signerBottom.Sender(signedTx)
			if chainIDTop == chainIDBottom {
				// Addresses should match, no error should be present
				assert.NoError(t, recoverErr)

				assert.Equal(t, recoveredSender.String(), PubKeyToAddress(&key.PublicKey).String())
			} else {
				// There should be an error for mismatched chain IDs
				assert.Error(t, recoverErr)
			}
		}
	}
}

func TestSignTransaction(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("5267eb25eb9bd1dffbb88e8cdf9cefa0a1b5f6db737e707c3ef8c7xxxxxxxxxxx")
	if err != nil {
		t.Fatal(err)
	}
	to := types.StringToAddress("0xBf3Aabb78e96c18a425C39D82C2B6505aA86940F")
	txn := &types.Transaction{
		To:       &to,
		Value:    big.NewInt(1000 * params.GWei),
		GasPrice: big.NewInt(300 * params.GWei),
		Gas:      25000,
		Nonce:    1,
	}

	signer := NewEIP155Signer(2000)

	signedTx, signErr := signer.SignTx(txn, privateKey)
	if signErr != nil {
		t.Fatalf("Unable to sign transaction")
	}

	t.Log(signedTx.Hash(), hex.EncodeToHex(signedTx.MarshalRLP()))
}

//curl -X POST -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":3,"method":"eth_sendRawTransaction","params":["0xf86b018545d964b8008261a894bf3aabb78e96c18a425c39d82c2b6505aa86940f85e8d4a5100080820fc4a04c1a8b186a487819be646711038790218036bfe336442a831d2d31adf4f492faa0298077e12743ce8f574af1a09b77f781564a9356689d84acde3600c4951d500d"]}' https://dogechain.ankr.com
