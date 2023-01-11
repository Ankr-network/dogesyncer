package server

import (
	"fmt"

	"github.com/ankr/dogesyncer/network"
	"github.com/spf13/cobra"
)

func SetFlags(cmd *cobra.Command) {
	defaultConfig := DefaultConfig()

	// grpc
	{
		cmd.Flags().String(
			GRPCAddressFlag,
			"127.0.0.1:9632",
			"the GRPC interface",
		)
	}

	// rpc & ws & graphQL

	{
		cmd.Flags().String(
			JsonrpcAddress,
			"127.0.0.1",
			"rpc address",
		)
		cmd.Flags().String(
			JsonrpcPort,
			"8545",
			"rpc port",
		)
		cmd.Flags().Bool(
			enableWSFlag,
			false,
			"the flag indicating that node enable websocket service",
		)
		cmd.Flags().String(
			websocketAddress,
			"127.0.0.1",
			"websocket address",
		)
		cmd.Flags().String(
			websocketPort,
			"8546",
			"websocket port",
		)
		cmd.Flags().Bool(
			enableGraphQLFlag,
			false,
			"the flag indicating that node enable graphQL service",
		)
		cmd.Flags().String(
			graphqlAddress,
			"127.0.0.1",
			"graphQL address",
		)
		cmd.Flags().String(
			graphqlPort,
			"8547",
			"graphQL port",
		)

	}

	// basic flags
	{
		cmd.Flags().StringVar(
			&params.configPath,
			configFlag,
			"",
			"the path to the CLI config. Supports .json and .hcl",
		)

		cmd.Flags().StringVar(
			&params.rawConfig.DataDir,
			dataDirFlag,
			defaultConfig.DataDir,
			"the data directory used for storing Dogechain-Lab Dogechain client data",
		)

		cmd.Flags().StringVar(
			&params.rawConfig.DbType,
			dbTypeFlag,
			defaultConfig.DbType,
			"the database type",
		)

		cmd.Flags().StringVar(
			&params.rawConfig.GenesisPath,
			genesisPathFlag,
			defaultConfig.GenesisPath,
			"the genesis file used for starting the chain",
		)

	}

	// block flags
	{
		cmd.Flags().Uint64Var(
			&params.rawConfig.BlockTime,
			blockTimeFlag,
			defaultConfig.BlockTime,
			"minimum block time in seconds (at least 1s)",
		)
	}

	// transaction flags
	{
		cmd.Flags().Uint64Var(
			&params.rawConfig.PriceLimit,
			priceLimitFlag,
			defaultConfig.PriceLimit,
			"price Limit, min value is 50000000000",
		)
	}

	// log flags
	{
		cmd.Flags().StringVar(
			&params.rawConfig.LogLevel,
			LogLevelFlag,
			defaultConfig.LogLevel,
			"the log level for console output",
		)

		cmd.Flags().StringVar(
			&params.rawConfig.LogFilePath,
			logFileLocationFlag,
			defaultConfig.LogFilePath,
			"write all logs to the file at specified location instead of writing them to console",
		)
	}

	// network flags
	{
		cmd.Flags().BoolVar(
			&params.rawConfig.Network.NoDiscover,
			NoDiscoverFlag,
			defaultConfig.Network.NoDiscover,
			"prevent the client from discovering other peers (default: false)",
		)

		cmd.Flags().Int64Var(
			&params.rawConfig.Network.MaxPeers,
			maxPeersFlag,
			-1,
			"the client's max number of peers allowed",
		)
		// override default usage value
		cmd.Flag(maxPeersFlag).DefValue = fmt.Sprintf("%d", defaultConfig.Network.MaxPeers)

		cmd.Flags().Int64Var(
			&params.rawConfig.Network.MaxInboundPeers,
			maxInboundPeersFlag,
			-1,
			"the client's max number of inbound peers allowed",
		)
		// override default usage value
		cmd.Flag(maxInboundPeersFlag).DefValue = fmt.Sprintf("%d", defaultConfig.Network.MaxInboundPeers)

		cmd.Flags().Int64Var(
			&params.rawConfig.Network.MaxOutboundPeers,
			maxOutboundPeersFlag,
			-1,
			"the client's max number of outbound peers allowed",
		)
		// override default usage value
		cmd.Flag(maxOutboundPeersFlag).DefValue = fmt.Sprintf("%d", defaultConfig.Network.MaxOutboundPeers)

		cmd.Flags().StringVar(
			&params.rawConfig.Network.Libp2pAddr,
			libp2pAddressFlag,
			fmt.Sprintf("127.0.0.1:%d", network.DefaultLibp2pPort),
			"the address and port for the libp2p service",
		)

		cmd.Flags().StringVar(
			&params.rawConfig.Network.NatAddr,
			natFlag,
			"",
			"the external address (address:port), as can be seen by peers",
		)

		cmd.Flags().StringVar(
			&params.rawConfig.Network.DNSAddr,
			dnsFlag,
			"",
			"the host DNS address which can be used by a remote peer for connection",
		)

		cmd.Flags().StringArrayVar(
			&params.corsAllowedOrigins,
			corsOriginFlag,
			defaultConfig.Headers.AccessControlAllowOrigins,
			"the CORS header indicating whether any JSON-RPC response can be shared with the specified origin",
		)
	}

}
