package server

import (
	"context"
	"github.com/ankr/dogesyncer/graphql"
	"net"

	"github.com/ankr/dogesyncer/helper/progress"
	"github.com/ankr/dogesyncer/protocol"
	"github.com/ankr/dogesyncer/rpc"
	"github.com/spf13/cobra"
	"github.com/sunvim/utils/grace"
)

const (
	JSONOutputFlag = "json"
)

func Run(cmd *cobra.Command, args []string) {
	ctx, svc := grace.New(context.Background())

	serverConfig := params.generateConfig()
	m, err := NewServer(ctx, serverConfig)
	if err != nil {
		panic(err)
	}

	m.logger.Info("start to syncer")
	syncer := protocol.NewSyncer(m.logger, m.network, m.blockchain, serverConfig.DataDir)
	syncer.Start(ctx)

	hub := &jsonRPCHub{
		Server:             m.network,
		restoreProgression: progress.NewProgressionWrapper(progress.ChainSyncRestore),
		Blockchain:         m.blockchain,
		Executor:           m.executor,
	}

	rpcServer := rpc.NewRpcServer(m.logger, m.blockchain, m.executor, serverConfig.RpcAddr, serverConfig.RpcPort, hub, m.network, serverConfig.PriceLimit)
	rpcServer.Start(ctx)

	address, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:9001")
	conf := &graphql.Config{
		Store:   hub,
		Addr:    address,
		ChainID: uint64(m.config.Chain.Params.ChainID),
		//AccessControlAllowOrigin: s.config.GraphQL.AccessControlAllowOrigin,
		//BlockRangeLimit:          s.config.GraphQL.BlockRangeLimit,
		//EnablePProf:              s.config.GraphQL.EnablePprof,
	}
	err = graphql.NewGraphQLService(m.logger, conf)
	if err != nil {
		m.logger.Error("register graphql error", err)
	}

	// register close function
	svc.Register(syncer.Close)
	svc.Register(m.Close)

	m.logger.Info("server boot over...")
	svc.Wait()
}

func PreRun(cmd *cobra.Command, _ []string) error {
	// Set the grpc, json and graphql ip:port bindings
	// The config file will have precedence over --flag
	params.setRawGRPCAddress(GetGRPCAddress(cmd))
	params.setRawRpcAddress(GetRPCAddress(cmd))
	params.setRawRpcPort(GetRPCPort(cmd))

	// Check if the config file has been specified
	// Config file settings will override JSON-RPC and GRPC address values
	if isConfigFileSpecified(cmd) {
		if err := params.initConfigFromFile(); err != nil {
			return err
		}
	}

	if err := params.validateFlags(); err != nil {
		return err
	}

	if err := params.initRawParams(); err != nil {
		return err
	}

	return nil
}

func (p *serverParams) initRawParams() error {
	if err := p.initBlockGasTarget(); err != nil {
		return err
	}

	if err := p.initSecretsConfig(); err != nil {
		return err
	}

	if err := p.initGenesisConfig(); err != nil {
		return err
	}

	if err := p.initDataDirLocation(); err != nil {
		return err
	}

	if err := p.initBlockTime(); err != nil {
		return err
	}

	p.initPeerLimits()
	p.initLogFileLocation()

	return p.initAddresses()
}

func isConfigFileSpecified(cmd *cobra.Command) bool {
	return cmd.Flags().Changed(configFlag)
}

func GetGRPCAddress(cmd *cobra.Command) string {
	if cmd.Flags().Changed("grpc") {
		// The legacy GRPC flag was set, use that value
		return cmd.Flag("grpc").Value.String()
	}

	return cmd.Flag(GRPCAddressFlag).Value.String()
}
func GetRPCAddress(cmd *cobra.Command) string {
	return cmd.Flag(JsonrpcAddress).Value.String()
}
func GetRPCPort(cmd *cobra.Command) string {
	return cmd.Flag(JsonrpcPort).Value.String()
}
