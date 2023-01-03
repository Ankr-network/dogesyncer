package protocol

import (
	"math/big"
	"sync"

	"github.com/ankr/dogesyncer/protocol/proto"
	"github.com/ankr/dogesyncer/types"
	"github.com/libp2p/go-libp2p-core/peer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// Status defines the up to date information regarding the peer
type Status struct {
	Difficulty *big.Int   // Current difficulty
	Hash       types.Hash // Latest block hash
	Number     uint64     // Latest block number
}

// Copy creates a copy of the status
func (s *Status) Copy() *Status {
	ss := new(Status)
	ss.Hash = s.Hash
	ss.Number = s.Number
	ss.Difficulty = new(big.Int).Set(s.Difficulty)

	return ss
}

// toProto converts a Status object to a proto.V1Status
func (s *Status) toProto() *proto.V1Status {
	return &proto.V1Status{
		Number:     s.Number,
		Hash:       s.Hash.String(),
		Difficulty: s.Difficulty.String(),
	}
}

// statusFromProto extracts a Status object from a passed in proto.V1Status
func statusFromProto(p *proto.V1Status) (*Status, error) {
	s := &Status{
		Hash:   types.StringToHash(p.Hash),
		Number: p.Number,
	}

	diff, ok := new(big.Int).SetString(p.Difficulty, 10)
	if !ok {
		return nil, ErrDecodeDifficulty
	}

	s.Difficulty = diff

	return s, nil
}

// SyncPeer is a representation of the peer the node is syncing with
type SyncPeer struct {
	peer   peer.ID
	conn   *grpc.ClientConn
	client proto.V1Client

	// Peer status might not be the latest block due to its asynchronous broadcast
	// mechanism. The goroutine would makes the sequence unpredictable.
	// So do not rely on its status for step by step watching syncing, especially
	// in a bad network status.
	// We would rather evolve the syncing protocol instead of patching too much for
	// v1 protocol.
	status     *Status
	statusLock sync.RWMutex
}

// Number returns the latest peer block height
func (s *SyncPeer) Number() uint64 {
	s.statusLock.RLock()
	defer s.statusLock.RUnlock()

	return s.status.Number
}

// IsClosed returns whether peer's connectivity has been closed
func (s *SyncPeer) IsClosed() bool {
	return s.conn.GetState() == connectivity.Shutdown
}

func (s *SyncPeer) ID() peer.ID {
	return s.peer
}

func (s *SyncPeer) Status() connectivity.State {
	return s.conn.GetState()
}

func (s *SyncPeer) updateStatus(status *Status) {
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

	s.status = status
}
