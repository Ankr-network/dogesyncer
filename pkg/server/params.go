package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"strings"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/hcl"

	"github.com/ankr/dogesyncer/chain"
	"github.com/ankr/dogesyncer/network"
	"github.com/ankr/dogesyncer/network/common"
	"github.com/ankr/dogesyncer/secrets"
	"github.com/ankr/dogesyncer/types"
	"github.com/multiformats/go-multiaddr"
)

const (
	configFlag                   = "config"
	genesisPathFlag              = "chain"
	dataDirFlag                  = "data-dir"
	dbTypeFlag                   = "db-type"
	libp2pAddressFlag            = "libp2p"
	natFlag                      = "nat"
	dnsFlag                      = "dns"
	sealFlag                     = "seal"
	maxPeersFlag                 = "max-peers"
	maxInboundPeersFlag          = "max-inbound-peers"
	maxOutboundPeersFlag         = "max-outbound-peers"
	priceLimitFlag               = "price-limit"
	maxSlotsFlag                 = "max-slots"
	pruneTickSecondsFlag         = "prune-tick-seconds"
	promoteOutdateSecondsFlag    = "promote-outdate-seconds"
	blockGasTargetFlag           = "block-gas-target"
	secretsConfigFlag            = "secrets-config"
	restoreFlag                  = "restore"
	blockTimeFlag                = "block-time"
	devIntervalFlag              = "dev-interval"
	devFlag                      = "dev"
	corsOriginFlag               = "access-control-allow-origins"
	daemonFlag                   = "daemon"
	logFileLocationFlag          = "log-to"
	enableGraphQLFlag            = "graphql"
	jsonRPCBatchRequestLimitFlag = "json-rpc-batch-request-limit"
	jsonRPCBlockRangeLimitFlag   = "json-rpc-block-range-limit"
	jsonrpcNamespaceFlag         = "json-rpc-namespace"
	JsonrpcAddress               = "http.addr"
	JsonrpcPort                  = "http.port"
	enableWSFlag                 = "ws"
	websocketAddress             = "ws.addr"
	websocketPort                = "ws.port"
	graphqlAddress               = "graphql.addr"
	graphqlPort                  = "graphql.port"
)

const (
	NoDiscoverFlag  = "no-discover"
	BootnodeFlag    = "bootnode"
	LogLevelFlag    = "log-level"
	GRPCAddressFlag = "grpc-address"
)

const (
	unsetPeersValue = -1
)

var (
	params = &serverParams{
		rawConfig: DefaultConfig(),
	}
)

var (
	errInvalidPeerParams = errors.New("both max-peers and max-inbound/outbound flags are set")
	errInvalidNATAddress = errors.New("could not parse NAT address (ip:port)")
)

type serverParams struct {
	rawConfig  *Config
	configPath string

	libp2pAddress     *net.TCPAddr
	prometheusAddress *net.TCPAddr
	natAddress        *net.TCPAddr
	dnsAddress        multiaddr.Multiaddr
	grpcAddress       *net.TCPAddr

	blockGasTarget uint64
	devInterval    uint64
	isDevMode      bool
	isDaemon       bool
	validatorKey   string

	corsAllowedOrigins []string

	genesisConfig *chain.Chain
	secretsConfig *secrets.SecretsManagerConfig

	logFileLocation string
}

func (p *serverParams) initConfigFromFile() error {
	var parseErr error

	if p.rawConfig, parseErr = readConfigFile(p.configPath); parseErr != nil {
		return parseErr
	}

	return nil
}

// readConfigFile reads the config file from the specified path, builds a Config object
// and returns it.
//
// Supported file types: .json, .hcl
func readConfigFile(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var unmarshalFunc func([]byte, interface{}) error

	switch {
	case strings.HasSuffix(path, ".hcl"):
		unmarshalFunc = hcl.Unmarshal
	case strings.HasSuffix(path, ".json"):
		unmarshalFunc = json.Unmarshal
	default:
		return nil, fmt.Errorf("suffix of %s is neither hcl nor json", path)
	}

	config := DefaultConfig()
	config.Network = new(Network)
	config.Network.MaxPeers = -1
	config.Network.MaxInboundPeers = -1
	config.Network.MaxOutboundPeers = -1

	if err := unmarshalFunc(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

func (p *serverParams) validateFlags() error {
	// Validate the max peers configuration
	if p.isMaxPeersSet() && p.isPeerRangeSet() {
		return errInvalidPeerParams
	}

	return nil
}

func (p *serverParams) isLogFileLocationSet() bool {
	return p.rawConfig.LogFilePath != ""
}

func (p *serverParams) isMaxPeersSet() bool {
	return p.rawConfig.Network.MaxPeers != unsetPeersValue
}

func (p *serverParams) isPeerRangeSet() bool {
	return p.rawConfig.Network.MaxInboundPeers != unsetPeersValue ||
		p.rawConfig.Network.MaxOutboundPeers != unsetPeersValue
}

func (p *serverParams) isSecretsConfigPathSet() bool {
	return p.rawConfig.SecretsConfigPath != ""
}

func (p *serverParams) isNATAddressSet() bool {
	return p.rawConfig.Network.NatAddr != ""
}

func (p *serverParams) isDNSAddressSet() bool {
	return p.rawConfig.Network.DNSAddr != ""
}

func (p *serverParams) setRawGRPCAddress(grpcAddress string) {
	p.rawConfig.GRPCAddr = grpcAddress
}

func (p *serverParams) setRawRpcAddress(rpcAddress string) {
	p.rawConfig.HttpAddr = rpcAddress
}

func (p *serverParams) setRawRpcPort(rpcPort string) {
	p.rawConfig.HttpPort = rpcPort
}

func (p *serverParams) setRawWebsocket(enable bool) {
	p.rawConfig.EnableWebsocket = enable
}

func (p *serverParams) setRawWebsocketAddress(address string) {
	p.rawConfig.WebsocketAddr = address
}

func (p *serverParams) setRawWebsocketPort(port string) {
	p.rawConfig.WebsocketPort = port
}

func (p *serverParams) setRawGraphQL(enable bool) {
	p.rawConfig.EnableGraphQL = enable
}

func (p *serverParams) setRawGraphQLAddress(address string) {
	p.rawConfig.GraphQLAddr = address
}

func (p *serverParams) setRawGraphQLPort(port string) {
	p.rawConfig.GraphQLPort = port
}

func (p *serverParams) generateConfig() *ServerConfig {
	chainCfg := p.genesisConfig

	// Replace block gas limit
	if p.blockGasTarget > 0 {
		chainCfg.Params.BlockGasTarget = p.blockGasTarget
	}

	// namespace

	return &ServerConfig{
		Chain:           chainCfg,
		GRPCAddr:        p.grpcAddress,
		LibP2PAddr:      p.libp2pAddress,
		RpcAddr:         p.rawConfig.HttpAddr,
		RpcPort:         p.rawConfig.HttpPort,
		EnableWebsocket: p.rawConfig.EnableWebsocket,
		WebsocketAddr:   p.rawConfig.WebsocketAddr,
		WebsocketPort:   p.rawConfig.WebsocketPort,
		EnableGraphQL:   p.rawConfig.EnableGraphQL,
		GraphQLAddr:     p.rawConfig.GraphQLAddr,
		GraphQLPort:     p.rawConfig.GraphQLPort,
		Network: &network.Config{
			NoDiscover:       p.rawConfig.Network.NoDiscover,
			Addr:             p.libp2pAddress,
			NatAddr:          p.natAddress,
			DNS:              p.dnsAddress,
			DataDir:          p.rawConfig.DataDir,
			MaxPeers:         p.rawConfig.Network.MaxPeers,
			MaxInboundPeers:  p.rawConfig.Network.MaxInboundPeers,
			MaxOutboundPeers: p.rawConfig.Network.MaxOutboundPeers,
			Chain:            p.genesisConfig,
		},
		DataDir:        p.rawConfig.DataDir,
		DbType:         p.rawConfig.DbType,
		SecretsManager: p.secretsConfig,
		BlockTime:      p.rawConfig.BlockTime,
		LogLevel:       hclog.LevelFromString(p.rawConfig.LogLevel),
		LogFilePath:    p.logFileLocation,
		Daemon:         p.isDaemon,
		ValidatorKey:   p.validatorKey,
	}
}

func (p *serverParams) initBlockGasTarget() error {
	var parseErr error

	if p.blockGasTarget, parseErr = types.ParseUint64orHex(
		&p.rawConfig.BlockGasTarget,
	); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initSecretsConfig() error {
	if !p.isSecretsConfigPathSet() {
		return nil
	}

	var parseErr error

	if p.secretsConfig, parseErr = secrets.ReadConfig(
		p.rawConfig.SecretsConfigPath,
	); parseErr != nil {
		return fmt.Errorf("unable to read secrets config file, %w", parseErr)
	}

	return nil
}

func (p *serverParams) initGenesisConfig() error {
	var parseErr error

	if p.genesisConfig, parseErr = chain.Import(
		p.rawConfig.GenesisPath,
	); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initPeerLimits() {
	if !p.isMaxPeersSet() && !p.isPeerRangeSet() {
		// No peer limits specified, use the default limits
		p.initDefaultPeerLimits()

		return
	}

	if p.isPeerRangeSet() {
		// Some part of the peer range is specified
		p.initUsingPeerRange()

		return
	}

	if p.isMaxPeersSet() {
		// The max peer value is specified, derive precise limits
		p.initUsingMaxPeers()

		return
	}
}

func (p *serverParams) initDefaultPeerLimits() {
	defaultNetworkConfig := network.DefaultConfig()

	p.rawConfig.Network.MaxPeers = defaultNetworkConfig.MaxPeers
	p.rawConfig.Network.MaxInboundPeers = defaultNetworkConfig.MaxInboundPeers
	p.rawConfig.Network.MaxOutboundPeers = defaultNetworkConfig.MaxOutboundPeers
}

func (p *serverParams) initUsingPeerRange() {
	defaultConfig := network.DefaultConfig()

	if p.rawConfig.Network.MaxInboundPeers == unsetPeersValue {
		p.rawConfig.Network.MaxInboundPeers = defaultConfig.MaxInboundPeers
	}

	if p.rawConfig.Network.MaxOutboundPeers == unsetPeersValue {
		p.rawConfig.Network.MaxOutboundPeers = defaultConfig.MaxOutboundPeers
	}

	p.rawConfig.Network.MaxPeers = p.rawConfig.Network.MaxInboundPeers + p.rawConfig.Network.MaxOutboundPeers
}

func (p *serverParams) initUsingMaxPeers() {
	p.rawConfig.Network.MaxOutboundPeers = int64(
		math.Floor(
			float64(p.rawConfig.Network.MaxPeers) * network.DefaultDialRatio,
		),
	)
	p.rawConfig.Network.MaxInboundPeers = p.rawConfig.Network.MaxPeers - p.rawConfig.Network.MaxOutboundPeers
}

var (
	errInvalidBlockTime       = errors.New("invalid block time specified")
	errDataDirectoryUndefined = errors.New("data directory not defined")
)

func (p *serverParams) initDataDirLocation() error {
	if p.rawConfig.DataDir == "" {
		return errDataDirectoryUndefined
	}

	return nil
}

func (p *serverParams) initBlockTime() error {
	if p.rawConfig.BlockTime < 1 {
		return errInvalidBlockTime
	}

	return nil
}

func (p *serverParams) initLogFileLocation() {
	if p.isLogFileLocationSet() {
		p.logFileLocation = p.rawConfig.LogFilePath
	}
}

func (p *serverParams) initAddresses() error {

	if err := p.initLibp2pAddress(); err != nil {
		return err
	}

	// need libp2p address to be set before initializing nat address
	if err := p.initNATAddress(); err != nil {
		return err
	}

	if err := p.initDNSAddress(); err != nil {
		return err
	}

	return p.initGRPCAddress()
}

func (p *serverParams) initLibp2pAddress() error {
	var parseErr error

	if p.libp2pAddress, parseErr = ResolveAddr(
		p.rawConfig.Network.Libp2pAddr,
		"127.0.0.1",
	); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initNATAddress() error {
	if !p.isNATAddressSet() {
		return nil
	}

	var parseErr error
	p.natAddress, parseErr = net.ResolveTCPAddr("tcp", p.rawConfig.Network.NatAddr)

	if parseErr != nil {
		//compatible with no port setups
		fmt.Printf("%s, use libp2p port\n", parseErr)

		oldNatAddrCfg := net.ParseIP(p.rawConfig.Network.NatAddr)
		if oldNatAddrCfg != nil {
			p.natAddress, parseErr = net.ResolveTCPAddr("tcp",
				fmt.Sprintf("%s:%d", oldNatAddrCfg.String(), p.libp2pAddress.Port),
			)
			if parseErr == nil {
				return nil
			}
		}

		return errInvalidNATAddress
	}

	return nil
}

func (p *serverParams) initDNSAddress() error {
	if !p.isDNSAddressSet() {
		return nil
	}

	var parseErr error

	if p.dnsAddress, parseErr = common.MultiAddrFromDNS(
		p.rawConfig.Network.DNSAddr, p.libp2pAddress.Port,
	); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initGRPCAddress() error {
	var parseErr error

	if p.grpcAddress, parseErr = ResolveAddr(
		p.rawConfig.GRPCAddr,
		"127.0.0.1",
	); parseErr != nil {
		return parseErr
	}

	return nil
}

// ResolveAddr resolves the passed in TCP address
// The second param is the default ip to bind to, if no ip address is specified
func ResolveAddr(address string, defaultIP string) (*net.TCPAddr, error) {
	addr, err := net.ResolveTCPAddr("tcp", address)

	if err != nil {
		return nil, fmt.Errorf("failed to parse addr '%s': %w", address, err)
	}

	if addr.IP == nil {
		addr.IP = net.ParseIP(defaultIP)
	}

	return addr, nil
}
