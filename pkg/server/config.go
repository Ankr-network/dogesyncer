package server

import (
	"net"

	"github.com/ankr/dogesyncer/chain"
	"github.com/ankr/dogesyncer/network"
	"github.com/ankr/dogesyncer/secrets"
	"github.com/hashicorp/go-hclog"
)

// Config defines the server configuration params
type Config struct {
	GenesisPath       string   `json:"chain_config"`
	SecretsConfigPath string   `json:"secrets_config"`
	DataDir           string   `json:"data_dir"`
	BlockGasTarget    string   `json:"block_gas_target"`
	GRPCAddr          string   `json:"grpc_addr"`
	HttpAddr          string   `json:"rpc_addr"`
	HttpPort          string   `json:"rpc_port"`
	Network           *Network `json:"network"`
	LogLevel          string   `json:"log_level"`
	BlockTime         uint64   `json:"block_time_s"`
	Headers           *Headers `json:"headers"`
	LogFilePath       string   `json:"log_to"`
}

func DefaultConfig() *Config {
	defaultNetworkConfig := network.DefaultConfig()
	return &Config{
		GenesisPath:    "genesis.json",
		DataDir:        "dogechain",
		BlockGasTarget: "0x00",
		LogLevel:       "INFO",
		HttpAddr:       "127.0.0.1",
		HttpPort:       "8545",
		BlockTime:      2,
		Network: &Network{
			NoDiscover:       defaultNetworkConfig.NoDiscover,
			MaxPeers:         defaultNetworkConfig.MaxInboundPeers,
			MaxOutboundPeers: defaultNetworkConfig.MaxOutboundPeers,
			MaxInboundPeers:  defaultNetworkConfig.MaxInboundPeers,
		},
		Headers: &Headers{
			AccessControlAllowOrigins: []string{"*"},
		},
		LogFilePath: "",
	}
}

// Network defines the network configuration params
type Network struct {
	NoDiscover       bool   `json:"no_discover"`
	Libp2pAddr       string `json:"libp2p_addr"`
	NatAddr          string `json:"nat_addr"`
	DNSAddr          string `json:"dns_addr"`
	MaxPeers         int64  `json:"max_peers,omitempty"`
	MaxOutboundPeers int64  `json:"max_outbound_peers,omitempty"`
	MaxInboundPeers  int64  `json:"max_inbound_peers,omitempty"`
}

// Headers defines the HTTP response headers required to enable CORS.
type Headers struct {
	AccessControlAllowOrigins []string `json:"access_control_allow_origins"`
}

type ServerConfig struct {
	Chain *chain.Chain

	EnableGraphQL bool
	GRPCAddr      *net.TCPAddr
	LibP2PAddr    *net.TCPAddr
	RpcAddr       string
	RpcPort       string

	PriceLimit            uint64
	MaxSlots              uint64
	BlockTime             uint64
	PruneTickSeconds      uint64
	PromoteOutdateSeconds uint64

	Network *network.Config

	DataDir     string
	RestoreFile *string

	Seal           bool
	SecretsManager *secrets.SecretsManagerConfig

	LogLevel    hclog.Level
	LogFilePath string

	Daemon       bool
	ValidatorKey string
}
