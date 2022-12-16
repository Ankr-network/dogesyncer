package rawdb

import (
	"bufio"
	"bytes"
	"fmt"
	"math/big"

	"github.com/sunvim/dogesyncer/ethdb"
	"github.com/sunvim/dogesyncer/helper"
	"github.com/sunvim/dogesyncer/types"
)

func ReadTD(db ethdb.Database, hash types.Hash) (*big.Int, bool) {
	v, ok, err := db.Get(ethdb.TDDBI, hash[:])
	if err != nil || !ok {
		return nil, false
	}

	x, _ := helper.DecodeVarint(v)
	return big.NewInt(int64(x)), true

}

func WriteTD(db ethdb.Database, hash types.Hash, number uint64) error {
	return db.Set(ethdb.TDDBI, hash[:], helper.EncodeVarint(number))
}

func ReadHeadNumber(db ethdb.Database) (uint64, bool) {

	v, ok, err := db.Get(ethdb.AssistDBI, latestBlockNumber)
	if err != nil {
		return 0, false
	}
	number, _ := helper.DecodeVarint(v)
	return number, ok

}

func WriteHeadNumber(db ethdb.Database, number uint64) error {
	return db.Set(ethdb.AssistDBI, latestBlockNumber, helper.EncodeVarint(number))
}

func ReadHeadHash(db ethdb.Database) (types.Hash, bool) {

	v, ok, err := db.Get(ethdb.AssistDBI, latestBlockHash)
	if err != nil {
		return types.Hash{}, false
	}
	return types.BytesToHash(v), ok

}

func WriteHeadHash(db ethdb.Database, hash types.Hash) error {
	return db.Set(ethdb.AssistDBI, latestBlockHash, hash.Bytes())
}

func WriteBlockByHash(db ethdb.Database, hash types.Hash, block *types.Block) error {

	return nil
}

func ReadBlockByHash(db ethdb.Database, hash types.Hash) (*types.Block, bool) {
	return nil, false
}

func ReadCanonicalHash(db ethdb.Database, number uint64) (types.Hash, bool) {
	v, ok, _ := db.Get(ethdb.NumHashDBI, helper.EncodeVarint(number))
	if !ok {
		return types.Hash{}, false
	}
	return types.BytesToHash(v), true
}

func WriteCanonicalHash(db ethdb.Database, number uint64, hash types.Hash) error {
	return db.Set(ethdb.NumHashDBI, helper.EncodeVarint(number), hash.Bytes())
}

func WriteHeader(db ethdb.Database, header *types.Header) error {
	return db.Set(ethdb.HeadDBI, header.Hash.Bytes(), header.MarshalRLPTo(nil))
}

func ReadHeader(db ethdb.Database, hash types.Hash) (*types.Header, error) {

	header := &types.Header{}
	v, ok, err := db.Get(ethdb.HeadDBI, hash.Bytes())
	if err != nil {
		return header, err
	}

	if ok {
		err = header.UnmarshalRLP(v)
		if err != nil {
			return nil, err
		}
	}

	return header, nil
}

func WriteBody(db ethdb.Database, hash types.Hash, body *types.Body) error {
	if len(body.Transactions) == 0 {
		return nil
	}
	buf := helper.BufPool.Get().(*bytes.Buffer)
	defer helper.BufPool.Put(buf)
	buf.Reset()

	for _, v := range body.Transactions {
		buf.Write(v.Hash().Bytes())
	}

	return db.Set(ethdb.BodyDBI, hash[:], buf.Bytes())

}

func ReadBody(db ethdb.Database, hash types.Hash) ([]types.Hash, error) {

	v, ok, err := db.Get(ethdb.BodyDBI, hash[:])
	if err != nil {
		return nil, err
	}

	if ok {
		buf := helper.BufPool.Get().(*bytes.Buffer)
		defer helper.BufPool.Put(buf)
		buf.Reset()
		buf.Write(v)
		scanner := bufio.NewScanner(buf)
		scanner.Split(ScanHash)
		var rs []types.Hash
		for scanner.Scan() {
			rs = append(rs, types.BytesToHash(scanner.Bytes()))
		}
		return rs, nil
	}
	return nil, fmt.Errorf("body not found")
}

func WriteTransactions(db ethdb.Database, txes []*types.Transaction) error {

	batch := db.Batch()

	for _, tx := range txes {
		err := batch.Set(ethdb.TxesDBI, tx.Hash().Bytes(), tx.MarshalRLPTo(nil))
		if err != nil {
			return err
		}
	}

	if err := batch.Write(); err != nil {
		return err
	}

	return nil
}

func WriteTransaction(db ethdb.Database, tx *types.Transaction) error {
	return db.Set(ethdb.TxesDBI, tx.Hash().Bytes(), tx.MarshalRLPTo(nil))
}

func ReadTransaction(db ethdb.Database, hash types.Hash) (*types.Transaction, error) {
	v, ok, err := db.Get(ethdb.TxesDBI, hash.Bytes())
	if err != nil {
		return nil, err
	}

	if ok {
		tx := &types.Transaction{}
		err = tx.UnmarshalRLP(v)
		if err != nil {
			return nil, err
		}
		return tx, nil
	}
	return nil, fmt.Errorf("not found tx")
}

func WrteReceipts(db ethdb.Database, receipts types.Receipts) error {
	batch := db.Batch()

	for _, rx := range receipts {
		err := batch.Set(ethdb.ReceiptsDBI, rx.TxHash.Bytes(), rx.MarshalRLPTo(nil))
		if err != nil {
			return err
		}
	}

	if err := batch.Write(); err != nil {
		return err
	}
	return nil
}

func WrteReceipt(db ethdb.Database, receipt *types.Receipt) error {
	return db.Set(ethdb.ReceiptsDBI, receipt.TxHash.Bytes(), receipt.MarshalRLPTo(nil))
}

func ReadReceipt(db ethdb.Database, hash types.Hash) (*types.Receipt, error) {

	receipt := &types.Receipt{}

	v, ok, err := db.Get(ethdb.ReceiptsDBI, hash.Bytes())
	if err != nil {
		return nil, err
	}

	if ok {
		err = receipt.UnmarshalRLP(v)
		if err != nil {
			return nil, err
		}
	}

	return receipt, nil
}
