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
	"github.com/sunvim/dogesyncer/blockchain"
	"github.com/sunvim/dogesyncer/helper/progress"
	"github.com/sunvim/dogesyncer/network"
	"github.com/sunvim/dogesyncer/network/event"
	libp2pGrpc "github.com/sunvim/dogesyncer/network/grpc"
	"github.com/sunvim/dogesyncer/protocol/proto"
	"github.com/sunvim/dogesyncer/types"
	pq "github.com/sunvim/utils/priorityqueue"
	"google.golang.org/grpc/connectivity"
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

type NewBlockInfo struct {
	Number uint64
	Hash   types.Hash
}

func (n *NewBlockInfo) Compare(other pq.Item) int {
	o := other.(*NewBlockInfo)
	if o.Number < n.Number {
		return 1
	} else if o.Number == n.Number {
		return 0
	}
	return -1
}

// blocks sorted by number (ascending)

// Syncer is a sync protocol
type Syncer struct {
	logger     hclog.Logger
	blockchain blockchainShim

	peers *hashmap.Map[peer.ID, *SyncPeer] // Maps peer.ID -> SyncPeer

	serviceV1 *serviceV1
	stopCh    chan struct{}

	status     *Status
	statusLock sync.Mutex

	server          *network.Server
	syncProgression *progress.ProgressionWrapper

	// save new block info from remote peer
	enqueue   *pq.PriorityQueue
	enqueueCh chan struct{}
}

// NewSyncer creates a new Syncer instance
func NewSyncer(logger hclog.Logger, server *network.Server, blockchain blockchainShim) *Syncer {

	const defQueueSize = 8192

	s := &Syncer{
		logger:          logger.Named(_syncerName),
		stopCh:          make(chan struct{}),
		blockchain:      blockchain,
		server:          server,
		syncProgression: progress.NewProgressionWrapper(progress.ChainSyncBulk),
		peers:           hashmap.New[peer.ID, *SyncPeer](),
		enqueue:         pq.NewPriorityQueue(defQueueSize, false),
		enqueueCh:       make(chan struct{}, defQueueSize),
	}

	return s
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

		case <-s.stopCh:
			sub.Close()

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

	nb := &NewBlockInfo{
		Number: b.Number(),
		Hash:   b.Hash(),
	}

	s.enqueue.Put(nb)
	s.enqueueCh <- struct{}{}
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

}

func (s *Syncer) SyncWork(ctx context.Context) {
	s.logger.Info("starting to sync block ...")

	for {
		p := s.BestPeer()
		if p == nil {
			s.logger.Info("not found best peer")
			time.Sleep(30 * time.Second)
			continue
		}

		// find the common ancestor
		ancestor, _, err := s.findCommonAncestor(p.client, p.status)
		// check whether peer network same with us
		if isDifferentNetworkError(err) {
			s.server.DisconnectFromPeer(p.peer, "Different network")
		}

		// return error
		if err != nil {
			// No need to sync with this peer
			s.logger.Error("sync work can't find common ancestor", "err", err)
			continue
		}

		s.logger.Info("fork found", "ancestor", ancestor.Number, "target", p.status.Number, "peer", p.ID())
		// start to sync block

		// dynamic modifying syncing size
		blockAmount := int64(maxSkeletonHeadersAmount)
		var (
			lastTarget        uint64
			currentSyncHeight = ancestor.Number + 1
		)
		target := p.status.Number

		// Stop monitoring the sync progression upon exit

		for {
			if target == lastTarget {
				// there are no more changes to pull for now
				s.logger.Info("exit sync work!")
				return
			}
			if p.Status() != connectivity.Idle && p.Status() != connectivity.Ready {
				// there are no more changes to pull for now
				s.logger.Error("peer error not ready")
				break
			}

			stx := time.Now()

			headers, err := getHeaders(
				p.client,
				&proto.GetHeadersRequest{
					Number: int64(currentSyncHeight),
					Skip:   0,
					Amount: 190,
				},
			)
			if err != nil {
				s.logger.Error("header get", "err", err)
				return
			}
			s.logger.Info("get remote block", "currentSyncHeight", currentSyncHeight, "amount", blockAmount, "elapse", time.Since(stx))

			// increase block amount when succeeded
			blockAmount++
			if blockAmount > maxSkeletonHeadersAmount {
				blockAmount = maxSkeletonHeadersAmount
			}

			for _, header := range headers {
				s.logger.Info("sync block", "block", header)
				currentSyncHeight++
			}

		}

	}
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
