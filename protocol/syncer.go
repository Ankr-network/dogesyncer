package protocol

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cornelk/hashmap"
	"github.com/hashicorp/go-hclog"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/smallnest/chanx"
	"github.com/sunvim/dogesyncer/blockchain"
	"github.com/sunvim/dogesyncer/helper/progress"
	"github.com/sunvim/dogesyncer/network"
	"github.com/sunvim/dogesyncer/network/event"
	libp2pGrpc "github.com/sunvim/dogesyncer/network/grpc"
	"github.com/sunvim/dogesyncer/protocol/proto"
	"github.com/sunvim/dogesyncer/types"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	_syncerName = "syncer"
	_syncerV1   = "/syncer/0.1"
)

const (
	maxEnqueueSize = 50
	popTimeout     = 10 * time.Second
)

var (
	ErrLoadLocalGenesisFailed = errors.New("failed to read local genesis")
	ErrMismatchGenesis        = errors.New("genesis does not match")
	ErrCommonAncestorNotFound = errors.New("header is nil")
	ErrForkNotFound           = errors.New("fork not found")
	ErrPopTimeout             = errors.New("timeout")
	ErrConnectionClosed       = errors.New("connection closed")
	ErrTooManyHeaders         = errors.New("unexpected more than 1 result")
	ErrDecodeDifficulty       = errors.New("failed to decode difficulty")
	ErrInvalidTypeAssertion   = errors.New("invalid type assertion")
)

// blocks sorted by number (ascending)

// Syncer is a sync protocol
type Syncer struct {
	logger     hclog.Logger
	blockchain blockchainShim

	peers *hashmap.Map[peer.ID, *SyncPeer] // Maps peer.ID -> SyncPeer

	serviceV1 *serviceV1

	status     *Status
	statusLock sync.Mutex

	server          *network.Server
	syncProgression *progress.ProgressionWrapper

	// save new block info from remote peer
	enqueue   *PriorityQueue
	enqueueCh *chanx.UnboundedChan[struct{}]
	stxRecv   bool
	onceSend  *sync.Once
	stopSync  chan struct{}
}

// NewSyncer creates a new Syncer instance
func NewSyncer(logger hclog.Logger, server *network.Server, blockchain blockchainShim, datadir string) *Syncer {

	const defQueueSize = 819200
	s := &Syncer{
		logger:          logger.Named(_syncerName),
		blockchain:      blockchain,
		server:          server,
		syncProgression: progress.NewProgressionWrapper(progress.ChainSyncBulk),
		peers:           hashmap.New[peer.ID, *SyncPeer](),
		enqueue:         NewPriorityQueue(defQueueSize, false),
		enqueueCh:       chanx.NewUnboundedChan[struct{}](defQueueSize),
		onceSend:        &sync.Once{},
		stopSync:        make(chan struct{}),
	}

	return s
}

func (s *Syncer) Close() error {
	close(s.stopSync)
	return nil
}

// GetSyncProgression returns the latest sync progression, if any
func (s *Syncer) GetSyncProgression() *progress.Progression {
	return s.syncProgression.GetProgression()
}

// syncCurrentStatus taps into the blockchain event steam and updates the Syncer.status field
func (s *Syncer) syncCurrentStatus(ctx context.Context) {
	sub := s.blockchain.SubscribeEvents()
	eventCh := sub.GetEventCh()

	// watch the subscription and notify
	for {
		select {
		case evnt := <-eventCh:
			if evnt.Type == blockchain.EventFork {
				// we do not want to notify forks
				continue
			}

			if len(evnt.NewChain) == 0 {
				// this should not happen
				continue
			}

			status := &Status{
				Difficulty: evnt.Difficulty,
				Hash:       evnt.NewChain[0].Hash,
				Number:     evnt.NewChain[0].Number,
			}

			s.updateStatus(status)

			return
		}
	}
}

func (s *Syncer) updateStatus(status *Status) {
	s.statusLock.Lock()
	defer s.statusLock.Unlock()

	// compare current status, would only update until new height meet or fork happens
	switch {
	case status.Number < s.status.Number:
		return
	case status.Number == s.status.Number:
		if status.Hash == s.status.Hash {
			return
		}
	}

	s.logger.Debug("update syncer status", "status", status)

	s.status = status
}

// enqueueBlock adds the specific block to the peerID queue
func (s *Syncer) enqueueBlock(peerID peer.ID, b *types.Block) {
	s.logger.Debug("enqueue block", "peer", peerID, "number", b.Number(), "hash", b.Hash())

	if s.blockchain.Header().Number >= b.Number() {
		return
	}

	s.enqueue.Put(b)
	s.enqueueCh.In <- struct{}{}
}

func (s *Syncer) updatePeerStatus(peerID peer.ID, status *Status) {

	s.logger.Debug(
		"update peer status",
		"peer",
		peerID,
		"latest block number",
		status.Number,
		"latest block hash",
		status.Hash, "difficulty",
		status.Difficulty,
	)

	if syncPeer, ok := s.peers.Get(peerID); ok {
		syncPeer.updateStatus(status)
	}
}

// Broadcast broadcasts a block to all peers
func (s *Syncer) Broadcast(b *types.Block) {

	sendNotify := func(peerID peer.ID, syncPeer *SyncPeer, req *proto.NotifyReq) {
		startTime := time.Now()

		if _, err := syncPeer.client.Notify(context.Background(), req); err != nil {
			s.logger.Error("failed to notify", "err", err)

			return
		}

		duration := time.Since(startTime)

		s.logger.Debug(
			"notifying peer",
			"id", peerID,
			"duration", duration.Seconds(),
		)
	}

	// Get the chain difficulty associated with block
	td, ok := s.blockchain.GetTD(b.Hash())
	if !ok {
		// not supposed to happen
		s.logger.Error("total difficulty not found", "block number", b.Number())

		return
	}

	// broadcast the new block to all the peers
	req := &proto.NotifyReq{
		Status: &proto.V1Status{
			Hash:       b.Hash().String(),
			Number:     b.Number(),
			Difficulty: td.String(),
		},
		Raw: &anypb.Any{
			Value: b.MarshalRLP(),
		},
	}

	s.logger.Debug("broadcast start")
	s.peers.Range(func(peerID peer.ID, peer *SyncPeer) bool {
		go sendNotify(peerID, peer, req)
		return true
	})

	s.logger.Debug("broadcast end")
}

// Start starts the syncer protocol
func (s *Syncer) Start(ctx context.Context) {
	s.serviceV1 = &serviceV1{
		syncer: s,
		logger: s.logger.With("name", "serviceV1"),
		store:  s.blockchain,
	}

	// Get the current status of the syncer
	currentHeader := s.blockchain.Header()
	diff, _ := s.blockchain.GetTD(currentHeader.Hash)

	s.status = &Status{
		Hash:       currentHeader.Hash,
		Number:     currentHeader.Number,
		Difficulty: diff,
	}

	// Run the blockchain event listener loop
	go s.syncCurrentStatus(ctx)

	// Register the grpc protocol for syncer
	grpcStream := libp2pGrpc.NewGrpcStream()
	proto.RegisterV1Server(grpcStream.GrpcServer(), s.serviceV1)
	grpcStream.Serve()
	s.server.RegisterProtocol(_syncerV1, grpcStream)

	s.setupPeers()

	go s.handlePeerEvent(ctx)

	go s.SyncWork(ctx)

	go s.WatchSync(ctx)

}

func (s *Syncer) WatchSync(ctx context.Context) {
	defer s.logger.Info("exit handle new block")

	var (
		newblock = &types.Block{}
	)

	for {
		select {
		case <-s.stopSync:
			return
		case <-s.enqueueCh.Out:
			items, err := s.enqueue.Get(1)
			if err != nil {
				s.logger.Error("watch sync", "err", err)
				continue
			}
			newblock = items[0]
			stx := time.Now()
			err = s.blockchain.WriteBlock(newblock)
			if err != nil {
				s.logger.Error("handle new block", "err", err)
				return
			}
			s.logger.Info("write block", "time", time.Since(stx))
		}
	}
}

const (
	syncFinishedSize          = 16
	maxSkeletonHeadersAmount  = 190
	stepSkeletonHeadersAmount = 30
)

// number: 687 have tx
func (s *Syncer) SyncWork(ctx context.Context) {
	s.logger.Info("starting to sync block ...")
	defer s.logger.Info("exit sync work!")

	var (
		p        *SyncPeer
		ancestor *types.Header
		err      error
		blockCh  = make(chan []*types.Block, 4096)
	)

	for {
		select {
		case <-s.stopSync:
			return
		default:
			p = s.BestPeer()
			if p == nil {
				s.logger.Info("not found best peer")
				time.Sleep(10 * time.Second)
				continue
			}

			// find the common ancestor
			ancestor, _, err = s.findCommonAncestor(p.client, p.status)
			// check whether peer network same with us
			if isDifferentNetworkError(err) {
				s.server.DisconnectFromPeer(p.peer, "Different network")
			}

			// return error
			if err != nil {
				continue
			}

			s.logger.Info("fork found", "ancestor", ancestor.Number, "target", p.status.Number, "peer", p.ID())

			// start to revieve new block
			if ancestor.Number+syncFinishedSize > p.status.Number {
				s.StartToRecieveNewBlock()
			}

			var (
				target            uint64 = p.status.Number
				currentSyncHeight        = ancestor.Number + 1
			)

			// sync finished
			if currentSyncHeight == target {
				close(blockCh)
				return
			}
			blockAmount := maxSkeletonHeadersAmount
			for {
				sk := &skeleton{
					server: s.server,
					amount: int64(blockAmount),
				}

				blocks, err := sk.GetBlocks(ctx, p.ID(), currentSyncHeight)
				if err != nil {
					if rpcErr, ok := grpcstatus.FromError(err); ok {
						// the data size exceeds grpc server/client message size
						if rpcErr.Code() == grpccodes.ResourceExhausted {
							blockAmount /= 2

							continue
						}
					}
					break
				}
				blockAmount = maxSkeletonHeadersAmount

				for _, block := range blocks {
					err = s.blockchain.WriteBlock(block)
					if err != nil {
						s.logger.Error("write block", "err", err)
						return
					}
				}

				currentSyncHeight += uint64(len(blocks))

				// check again
				if currentSyncHeight >= target {
					continue
				}
			}
		}

	}
}

func (s *Syncer) StartToRecieveNewBlock() {
	s.onceSend.Do(func() {
		s.stxRecv = true
	})
}

// setupPeers adds connected peers as syncer peers
func (s *Syncer) setupPeers() {

	for _, p := range s.server.Peers() {
		if addErr := s.AddPeer(p.Info.ID); addErr != nil {
			s.logger.Error(fmt.Sprintf("Error when adding peer [%s], %v", p.Info.ID, addErr))
		}
	}

}

// handlePeerEvent subscribes network event and adds/deletes peer from syncer
func (s *Syncer) handlePeerEvent(ctx context.Context) {
	updateCh, err := s.server.SubscribeCh()
	if err != nil {
		s.logger.Error("failed to subscribe", "err", err)

		return
	}

	go func(ctx context.Context) {
		for {
			evnt, ok := <-updateCh
			if !ok {
				return
			}

			switch evnt.Type {
			case event.PeerConnected:
				if err := s.AddPeer(evnt.PeerID); err != nil {
					s.logger.Error("failed to add peer", "err", err)
				}
			case event.PeerDisconnected:
				if err := s.DeletePeer(evnt.PeerID); err != nil {
					s.logger.Error("failed to delete user", "err", err)
				}
			}
		}
	}(ctx)
}

// BestPeer returns the best peer by difficulty (if any)
func (s *Syncer) BestPeer() *SyncPeer {

	var (
		bestPeer        *SyncPeer
		bestBlockNumber uint64
	)

	s.peers.Range(func(peerID peer.ID, sp *SyncPeer) bool {
		peerBlockNumber := sp.Number()
		// compare block height
		if bestPeer == nil || peerBlockNumber > bestBlockNumber {
			bestPeer = sp
			bestBlockNumber = peerBlockNumber
		}
		return true
	})

	if bestBlockNumber <= s.blockchain.Header().Number {
		bestPeer = nil
	}

	return bestPeer
}

func (s *Syncer) TakePeerByHeight(height, num uint64) []*SyncPeer {
	var (
		rs    = make([]*SyncPeer, num)
		count uint64
	)

	s.peers.Range(func(peerID peer.ID, sp *SyncPeer) bool {
		if sp.Number() > height {
			rs[count] = sp
		}
		count++
		if count >= num {
			return false
		}
		return true
	})

	return rs
}

// AddPeer establishes new connection with the given peer
func (s *Syncer) AddPeer(peerID peer.ID) error {

	if _, ok := s.peers.Get(peerID); ok {
		// already connected
		return nil
	}

	stream, err := s.server.NewStream(_syncerV1, peerID)
	if err != nil {
		return fmt.Errorf("failed to open a stream, err %w", err)
	}

	conn := libp2pGrpc.WrapClient(stream)

	// watch for changes of the other node first
	clt := proto.NewV1Client(conn)

	rawStatus, err := clt.GetCurrent(context.Background(), &emptypb.Empty{})
	if err != nil {
		return err
	}

	status, err := statusFromProto(rawStatus)

	if err != nil {
		return err
	}

	s.peers.Set(peerID, &SyncPeer{
		peer:   peerID,
		conn:   conn,
		client: clt,
		status: status,
	})

	return nil
}

// DeletePeer deletes a peer from syncer
func (s *Syncer) DeletePeer(peerID peer.ID) error {

	p, ok := s.peers.Get(peerID)
	if ok {
		s.peers.Del(peerID)
		if err := p.conn.Close(); err != nil {
			return err
		}
	}

	return nil
}

// findCommonAncestor returns the common ancestor header and fork
func (s *Syncer) findCommonAncestor(clt proto.V1Client, status *Status) (*types.Header, *types.Header, error) {
	h := s.blockchain.Header()

	min := uint64(0) // genesis
	max := h.Number

	targetHeight := status.Number

	if heightNumber := targetHeight; max > heightNumber {
		max = heightNumber
	}

	var header *types.Header

	for min <= max {
		m := uint64(math.Floor(float64(min+max) / 2))

		if m == 0 {
			// our common ancestor is the genesis
			genesis, ok := s.blockchain.GetHeaderByNumber(0)
			if !ok {
				return nil, nil, ErrLoadLocalGenesisFailed
			}

			header = genesis

			break
		}

		found, err := getHeader(clt, &m, nil)
		if err != nil {
			return nil, nil, err
		}

		if found == nil {
			// peer does not have the m peer, search in lower bounds
			max = m - 1
		} else {
			expectedHeader, ok := s.blockchain.GetHeaderByNumber(m)
			if !ok {
				return nil, nil, fmt.Errorf("cannot find the header %d in local chain", m)
			}
			if expectedHeader.Hash == found.Hash {
				header = found
				min = m + 1
			} else {
				if m == 0 {
					return nil, nil, ErrMismatchGenesis
				}
				max = m - 1
			}
		}
	}

	if header == nil {
		return nil, nil, ErrCommonAncestorNotFound
	}

	// get the block fork
	forkNum := header.Number + 1
	fork, err := getHeader(clt, &forkNum, nil)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to get fork at num %d", header.Number)
	}

	if fork == nil {
		return nil, nil, ErrForkNotFound
	}

	return header, fork, nil
}

// WatchSyncWithPeer subscribes and adds peer's latest block
func (s *Syncer) logSyncPeerPopBlockError(err error, peer *SyncPeer) {
	if errors.Is(err, ErrPopTimeout) {
		msg := "failed to pop block within %ds from peer: id=%s, please check if all the validators are running"
		s.logger.Warn(fmt.Sprintf(msg, int(popTimeout.Seconds()), peer.peer))
	} else {
		s.logger.Info("failed to pop block from peer", "id", peer.peer, "err", err)
	}
}

func isDifferentNetworkError(err error) bool {
	if err == nil {
		return false
	}

	switch {
	case errors.Is(err, ErrMismatchGenesis), // genesis not right
		errors.Is(err, ErrCommonAncestorNotFound), // might be data missing
		errors.Is(err, ErrForkNotFound):           // starting block not found
		return true
	}

	return false
}

func getHeader(clt proto.V1Client, num *uint64, hash *types.Hash) (*types.Header, error) {
	req := &proto.GetHeadersRequest{}
	if num != nil {
		req.Number = int64(*num)
	}

	if hash != nil {
		req.Hash = (*hash).String()
	}

	resp, err := clt.GetHeaders(context.Background(), req)
	if err != nil {
		return nil, err
	}

	if len(resp.Objs) == 0 {
		return nil, nil
	}

	if len(resp.Objs) != 1 {
		return nil, ErrTooManyHeaders
	}

	obj := resp.Objs[0]

	if obj == nil || obj.Spec == nil || len(obj.Spec.Value) == 0 {
		return nil, errNilHeaderResponse
	}

	header := &types.Header{}

	if err := header.UnmarshalRLP(obj.Spec.Value); err != nil {
		return nil, err
	}

	return header, nil
}
