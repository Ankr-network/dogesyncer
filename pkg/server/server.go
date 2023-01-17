package server

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"

	"github.com/ankr/dogesyncer/blockchain"
	"github.com/ankr/dogesyncer/chain"
	"github.com/ankr/dogesyncer/ethdb"
	"github.com/ankr/dogesyncer/ethdb/mdbx"
	"github.com/ankr/dogesyncer/ethdb/pebble"
	"github.com/ankr/dogesyncer/helper/common"
	"github.com/ankr/dogesyncer/helper/progress"
	"github.com/ankr/dogesyncer/network"
	"github.com/ankr/dogesyncer/pkg/server/proto"
	"github.com/ankr/dogesyncer/secrets"
	"github.com/ankr/dogesyncer/state"
	itrie "github.com/ankr/dogesyncer/state/immutable-trie"
	"github.com/ankr/dogesyncer/state/runtime"
	"github.com/ankr/dogesyncer/state/runtime/evm"
	"github.com/ankr/dogesyncer/state/runtime/precompiled"
	"github.com/ankr/dogesyncer/types"
	"github.com/hashicorp/go-hclog"
	"google.golang.org/grpc"
)

type Server struct {
	logger       hclog.Logger
	config       *ServerConfig
	state        state.State
	stateStorage itrie.Storage

	chain *chain.Chain

	blockchain *blockchain.Blockchain

	// state executor
	executor *state.Executor

	// system grpc server
	grpcServer *grpc.Server

	// libp2p network
	network *network.Server

	// secrets manager
	secretsManager secrets.SecretsManager
}

// NewServer creates a new Minimal server, using the passed in configuration
func NewServer(config *ServerConfig) (*Server, error) {
	logger, err := newLoggerFromConfig(config)
	if err != nil {
		return nil, fmt.Errorf("could not setup new logger instance, %w", err)
	}

	m := &Server{
		logger: logger,
		config: config,
		grpcServer: grpc.NewServer(
			grpc.MaxRecvMsgSize(common.MaxGrpcMsgSize),
			grpc.MaxSendMsgSize(common.MaxGrpcMsgSize),
		),
	}

	m.logger.Info("Data dir", "path", config.DataDir)

	// Set up the secrets manager
	if err := m.setupSecretsManager(); err != nil {
		return nil, fmt.Errorf("failed to set up the secrets manager: %w", err)
	}

	// start libp2p
	{
		netConfig := config.Network
		netConfig.DataDir = filepath.Join(m.config.DataDir, "libp2p")
		netConfig.SecretsManager = m.secretsManager
		netConfig.Metrics = network.NilMetrics()

		network, err := network.NewServer(logger, netConfig)
		if err != nil {
			return nil, err
		}
		m.network = network
	}

	// create database
	var db ethdb.Database

	if config.DbType == "pebble" {
		db = pebble.NewPebbleDB(filepath.Join(config.DataDir, "blockchain"), logger.Named("pebble"))
	} else {
		db = mdbx.NewMDBX(filepath.Join(config.DataDir, "blockchain"), logger.Named("mdbx"))
	}

	// start blockchain object
	stateStorage, err := func() (itrie.Storage, error) {
		return itrie.NewKVStorage(db), nil
	}()

	if err != nil {
		return nil, err
	}

	m.stateStorage = stateStorage

	st := itrie.NewState(stateStorage, nil)
	m.state = st

	m.executor = state.NewExecutor(config.Chain.Params, st, logger)
	m.executor.SetRuntime(precompiled.NewPrecompiled())
	m.executor.SetRuntime(evm.NewEVM())

	// compute the genesis root state
	genesisRoot := m.executor.WriteGenesis(config.Chain.Genesis.Alloc)
	config.Chain.Genesis.StateRoot = genesisRoot

	// blockchain
	m.blockchain, err = blockchain.NewBlockchain(logger, db, config.Chain, m.executor, st)
	if err != nil {
		return nil, err
	}

	m.executor.GetHash = m.blockchain.GetHashHelper

	err = m.blockchain.HandleGenesis()
	if err != nil {
		return nil, err
	}

	// setup and start grpc server
	if err := m.setupGRPC(); err != nil {
		return nil, err
	}

	if err := m.network.Start(); err != nil {
		return nil, err
	}

	m.blockchain.BloomIndexer.Start(m.blockchain)

	return m, nil
}

// setupGRPC sets up the grpc server and listens on tcp
func (s *Server) setupGRPC() error {
	proto.RegisterSystemServer(s.grpcServer, &systemService{server: s})

	lis, err := net.Listen("tcp", s.config.GRPCAddr.String())
	if err != nil {
		return err
	}

	go func() {
		if err := s.grpcServer.Serve(lis); err != nil {
			s.logger.Error(err.Error())
		}
	}()

	s.logger.Info("GRPC server running", "addr", s.config.GRPCAddr.String())

	return nil
}

// setupSecretsManager sets up the secrets manager
func (s *Server) setupSecretsManager() error {
	secretsManagerConfig := s.config.SecretsManager
	if secretsManagerConfig == nil {
		// No config provided, use default
		secretsManagerConfig = &secrets.SecretsManagerConfig{
			Type: secrets.Local,
		}
	}

	secretsManagerType := secretsManagerConfig.Type
	secretsManagerParams := &secrets.SecretsManagerParams{
		Logger: s.logger,
	}

	if secretsManagerType == secrets.Local {
		// The base directory is required for the local secrets manager
		secretsManagerParams.Extra = map[string]interface{}{
			secrets.Path: s.config.DataDir,
		}

		// When server started as daemon,
		// ValidatorKey is required for the local secrets manager
		if s.config.Daemon {
			secretsManagerParams.DaemonValidatorKey = s.config.ValidatorKey
			secretsManagerParams.IsDaemon = s.config.Daemon
		}
	}

	// Grab the factory method
	secretsManagerFactory, ok := secretsManagerBackends[secretsManagerType]
	if !ok {
		return fmt.Errorf("secrets manager type '%s' not found", secretsManagerType)
	}

	// Instantiate the secrets manager
	secretsManager, factoryErr := secretsManagerFactory(
		secretsManagerConfig,
		secretsManagerParams,
	)

	if factoryErr != nil {
		return fmt.Errorf("unable to instantiate secrets manager, %w", factoryErr)
	}

	s.secretsManager = secretsManager

	return nil
}

// JoinPeer attempts to add a new peer to the networking server
func (s *Server) JoinPeer(rawPeerMultiaddr string) error {
	return s.network.JoinPeer(rawPeerMultiaddr)
}

// Close closes the Minimal server (blockchain, networking, consensus)
func (s *Server) Close() error {

	s.logger.Info("closing network...")
	// Close the networking layer
	if err := s.network.Close(); err != nil {
		s.logger.Error("failed to close networking", "err", err.Error())
	}
	s.logger.Info("network close over")

	s.logger.Info("closing blockchain...")
	// Close the state storage
	if err := s.blockchain.Close(); err != nil {
		s.logger.Error("failed to close blockchain", "err", err)
	}
	s.logger.Info("close blockchain over")

	return nil

}

type jsonRPCHub struct {
	// state state.State
	restoreProgression *progress.ProgressionWrapper

	*blockchain.Blockchain
	// *txpool.TxPool
	*state.Executor
	*network.Server
}

func (j *jsonRPCHub) GetPeers() int {
	return len(j.Server.Peers())
}

func (j *jsonRPCHub) ApplyTxn(
	header *types.Header,
	txn *types.Transaction,
) (result *runtime.ExecutionResult, err error) {
	blockCreator, err := j.GetConsensus().GetBlockCreator(header)
	if err != nil {
		return nil, err
	}

	transition, err := j.BeginTxn(header.StateRoot, header, blockCreator)

	if err != nil {
		return
	}

	result, err = transition.Apply(txn)

	return
}

func (j *jsonRPCHub) GetSyncProgression() *progress.Progression {
	// restore progression
	if restoreProg := j.restoreProgression.GetProgression(); restoreProg != nil {
		return restoreProg
	}
	return nil
}

func (j *jsonRPCHub) StateAtTransaction(block *types.Block, txIndex int) (*state.Transition, error) {
	if block.Number() == 0 {
		return nil, errors.New("no transaction in genesis")
	}

	if txIndex < 0 {
		return nil, errors.New("invalid transaction index")
	}

	// get parent header
	parent, exists := j.GetParent(block.Header)
	if !exists {
		return nil, fmt.Errorf("parent %s not found", block.ParentHash())
	}

	// block creator
	blockCreator, err := j.GetConsensus().GetBlockCreator(block.Header)
	if err != nil {
		return nil, err
	}

	// begin transition, use parent block
	txn, err := j.BeginTxn(parent.StateRoot, parent, blockCreator)
	if err != nil {
		return nil, err
	}

	if txIndex == 0 {
		return txn, nil
	}

	for idx, tx := range block.Transactions {
		if idx == txIndex {
			return txn, nil
		}

		if _, err := txn.Apply(tx); err != nil {
			return nil, fmt.Errorf("transaction %s failed: %w", tx.Hash(), err)
		}
	}

	return nil, fmt.Errorf("transaction index %d out of range for block %s", txIndex, block.Hash())
}
