package protocol

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/sunvim/dogesyncer/network"
	"github.com/sunvim/dogesyncer/protocol/proto"
	"github.com/sunvim/dogesyncer/types"
)

const (
	defaultBodyFetchTimeout = time.Second * 10
)

var (
	errNilHeaderResponse     = errors.New("header response is nil")
	errInvalidHeaderSequence = errors.New("invalid header sequence")
	errHeaderBodyMismatch    = errors.New("requested body and header mismatch")
)

func getHeaders(clt proto.V1Client, req *proto.GetHeadersRequest) ([]*types.Header, error) {
	resp, err := clt.GetHeaders(context.Background(), req)
	if err != nil {
		return nil, err
	}

	headers := make([]*types.Header, len(resp.Objs))

	for index, obj := range resp.Objs {
		if obj == nil || obj.Spec == nil {
			// this nil header comes from a faulty node, reject all blocks of it.
			return nil, errNilHeaderResponse
		}

		header := &types.Header{}
		if err := header.UnmarshalRLP(obj.Spec.Value); err != nil {
			return nil, err
		}

		headers[index] = header
	}

	return headers, nil
}

type skeleton struct {
	server *network.Server
	blocks []*types.Block
	skip   int64
	amount int64
}

// getBlocksFromPeer fetches the blocks from the peer,
// from the specified block number (including)
func (s *skeleton) getBlocksFromPeer(
	peerClient proto.V1Client,
	initialBlockNum uint64,
) error {
	// Fetch the headers from the peer
	headers, err := getHeaders(
		peerClient,
		&proto.GetHeadersRequest{
			Number: int64(initialBlockNum),
			Skip:   s.skip,
			Amount: s.amount,
		},
	)
	if err != nil {
		return err
	}

	// Make sure the number sequences match up
	for i := 1; i < len(headers); i++ {
		if headers[i].Number-headers[i-1].Number != 1 {
			return errInvalidHeaderSequence
		}
	}

	// Construct the body request
	headerHashes := make([]types.Hash, len(headers))
	for index, header := range headers {
		headerHashes[index] = header.Hash
	}

	getBodiesContext, cancelFn := context.WithTimeout(
		context.Background(),
		defaultBodyFetchTimeout,
	)
	defer cancelFn()

	// Grab the block bodies
	bodies, err := getBodies(getBodiesContext, peerClient, headerHashes)
	if err != nil {
		return err
	}

	if len(bodies) != len(headers) {
		return errHeaderBodyMismatch
	}

	s.blocks = make([]*types.Block, len(headers))

	for index, body := range bodies {
		s.blocks[index] = &types.Block{
			Header:       headers[index],
			Transactions: body.Transactions,
		}
	}

	return nil
}

// GetBlocks returns a stream of blocks from given height to peer's latest
func (s *skeleton) GetBlocks(
	ctx context.Context,
	peerID peer.ID,
	from uint64,
) ([]*types.Block, error) {
	clt, err := newSyncPeerClient(s.server, peerID)
	if err != nil {
		return nil, fmt.Errorf("failed to create sync peer client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rsp, err := clt.GetBlocks(ctx, &proto.GetBlocksRequest{
		From: from,
		To:   from + uint64(s.amount),
	})
	if err != nil {
		return nil, err
	}

	blocks := make([]*types.Block, len(rsp.Blocks))

	for i, b := range rsp.Blocks {
		block := new(types.Block)

		if err := block.UnmarshalRLP(b); err != nil {
			return nil, fmt.Errorf("failed to UnmarshalRLP: %w", err)
		}

		blocks[i] = block
	}

	return blocks, err
}

func newSyncPeerClient(server *network.Server, peerID peer.ID) (proto.V1Client, error) {

	var err error

	conn := server.GetProtoStream(_syncerV1, peerID)
	if conn == nil {
		// create new connection
		conn, err = server.NewProtoConnection(_syncerV1, peerID)
		if err != nil {
			server.ForgetPeer(peerID, "not support syncer v1 protocol")

			return nil, fmt.Errorf("failed to open a stream, err %w", err)
		}

		// save protocol stream
		server.SaveProtocolStream(_syncerV1, conn, peerID)
	}

	return proto.NewV1Client(conn), nil
}
