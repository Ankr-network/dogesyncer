package server

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/sunvim/utils/grace"
)

const (
	JSONOutputFlag = "json"
)

func Run(cmd *cobra.Command, args []string) {
	_, svc := grace.New(context.Background())

	m, err := NewServer(params.generateConfig())
	if err != nil {
		panic(err)
	}
	svc.Register(m.Close)

	svc.Wait()
}

func PreRun(cmd *cobra.Command, _ []string) error {
	// Set the grpc, json and graphql ip:port bindings
	// The config file will have precedence over --flag
	params.setRawGRPCAddress(GetGRPCAddress(cmd))

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

	return cmd.Flag("grpc-address").Value.String()
}