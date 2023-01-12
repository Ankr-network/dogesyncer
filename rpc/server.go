package rpc

import (
	"context"
	"math/big"

	"github.com/ankr/dogesyncer/network"
	"github.com/dogechain-lab/dogechain/txpool/proto"

	"fmt"

	"github.com/ankr/dogesyncer/crypto"
	"github.com/ankr/dogesyncer/state"

	"github.com/ankr/dogesyncer/blockchain"
	"github.com/bytedance/sonic"
	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-hclog"
)

type endpoints struct {
	Eth  *Eth
	Web3 *Web3
	Net  *Net
}

const logBlockRange = 1024

type RpcServer struct {
	logger        hclog.Logger
	ctx           context.Context
	blockchain    *blockchain.Blockchain
	addr          string
	port          string
	routers       map[string]RpcFunc
	filterManager *FilterManager
	endpoints     endpoints
	executor      *state.Executor
	p2p           *network.Server
	topic         *network.Topic
	signer        *crypto.EIP155Signer
	gasLimit      uint64
	websocketAddr string
	websocketPort string
}

func NewRpcServer(logger hclog.Logger, blockchain *blockchain.Blockchain, executor *state.Executor, addr, port string,
	store JSONRPCStore, pspServer *network.Server, gasLimit uint64, wsAddr, wsPort string, priceLimit uint64) *RpcServer {
	topic, err := pspServer.NewTopic(topicNameV1, &proto.Txn{})
	if err != nil {
		panic(err)
	}

	s := &RpcServer{
		logger:        logger.Named("rpc"),
		addr:          addr,
		port:          port,
		blockchain:    blockchain,
		executor:      executor,
		filterManager: NewFilterManager(logger, blockchain, logBlockRange),
		signer:        crypto.NewEIP155Signer(uint64(blockchain.Config().Params.ChainID)),
		p2p:           pspServer,
		gasLimit:      gasLimit,
		topic:         topic,
		websocketAddr: wsAddr,
		websocketPort: wsPort,
	}
	go s.filterManager.Run()

	s.initEndpoints(store, priceLimit)
	s.initmethods()
	return s
}

func (s *RpcServer) Start(ctx context.Context) error {
	go func(ctx context.Context) {
		svc := fiber.New(fiber.Config{
			Prefork:               false,
			ServerHeader:          "doge syncer team",
			DisableStartupMessage: true,
			JSONEncoder:           sonic.Marshal,
			JSONDecoder:           sonic.Unmarshal,
		})

		ap := fmt.Sprintf("%s:%s", s.addr, s.port)
		s.logger.Info("boot", "address", s.addr, "port", s.port)

		// handle rpc request
		svc.Post("/", func(c *fiber.Ctx) error {

			c.Accepts("application/json")
			req := reqPool.Get().(*Request)
			defer reqPool.Put(req)
			err := c.BodyParser(req)
			if err != nil {
				s.logger.Error("route", "err", err)
				// c.Status(fiber.StatusOK).SendString("error request")
				rsp := resErrorPool.Get().(*ErrorResponse)
				defer resErrorPool.Put(rsp)
				errRes := NewInvalidParamsError("Invalid Params")
				rsp.Error = &ObjectError{errRes.ErrorCode(), errRes.Error(), nil}
				rsp.ID = req.ID
				rsp.Version = req.Version
				c.Status(fiber.StatusOK).JSON(rsp)
				return nil
			}

			exeMethod, ok := s.routers[req.Method]
			if !ok {
				s.logger.Error("route", "not support method", req.Method)
				c.Status(fiber.StatusBadRequest).SendString("not support method")
				return nil
			}

			res, errRes := exeMethod(req.Method, req.Params)
			if errRes != nil {
				rsp := resErrorPool.Get().(*ErrorResponse)
				defer resErrorPool.Put(rsp)
				rsp.Error = &ObjectError{errRes.ErrorCode(), errRes.Error(), nil}
				rsp.ID = req.ID
				rsp.Version = req.Version
				c.Status(fiber.StatusOK).JSON(rsp)
			} else {
				rsp := resPool.Get().(*Response)
				defer resPool.Put(rsp)
				rsp.Result = res
				rsp.ID = req.ID
				rsp.Version = req.Version
				c.Status(fiber.StatusOK).JSON(rsp)
			}

			return nil
		})

		err := svc.Listen(ap)
		if err != nil {
			return
		}
	}(ctx)

	return nil
}

func (s *RpcServer) initmethods() {
	s.routers = map[string]RpcFunc{
		"web3_clientVersion": s.Web3ClientVersion,
		"web3_sha3":          s.Web3Sha3,

		"net_version":   s.NetVersion,
		"net_listening": s.NetListening,
		"net_peerCount": s.NetPeerCount,

		"eth_getFilterLogs":       s.GetFilterLogs,
		"eth_getLogs":             s.GetLogs,
		"eth_uninstallFilter":     s.UninstallFilter,
		"eth_newFilter":           s.NewFilter,
		"eth_getBalance":          s.GetBalance,
		"eth_getCode":             s.GetCode,
		"eth_getStorageAt":        s.GetStorageAt,
		"eth_call":                s.Call,
		"eth_getTransactionCount": s.GetTransactionCount,
		"eth_estimateGas":         s.EstimateGas,
		"eth_sendRawTransaction":  s.SendRawTransaction,

		"eth_syncing":                             s.EthSyncing,
		"eth_gasPrice":                            s.EthGasPrice,
		"eth_blockNumber":                         s.GetBlockNumber,
		"eth_getBlockByHash":                      s.EthGetBlockByHash,
		"eth_getBlockByNumber":                    s.EthGetBlockByNumber,
		"eth_getTransactionByHash":                s.EthGetTransactionByHash,
		"eth_getTransactionByBlockNumberAndIndex": s.EthGetTransactionByBlockNumberAndIndex,
		"eth_getTransactionReceipt":               s.EthGetTransactionReceipt,
		"eth_chainId":                             s.NetVersion,
	}
}

func (s *RpcServer) initEndpoints(store JSONRPCStore, priceLimit uint64) {
	s.endpoints.Net = &Net{store: store, chainID: uint64(s.blockchain.Config().Params.ChainID)}
	s.endpoints.Web3 = &Web3{chainID: uint64(s.blockchain.Config().Params.ChainID)}

	priceLimitInt := new(big.Int).SetUint64(priceLimit)
	if priceLimitInt.Cmp(minGasPrice) == -1 {
		priceLimitInt = minGasPrice
	}

	s.endpoints.Eth = &Eth{
		store:      store,
		priceLimit: priceLimitInt,
	}
}

func (s *RpcServer) GetTxSigner(blockNumber uint64) crypto.TxSigner {
	var signer crypto.TxSigner
	forks := s.executor.GetForksInTime(blockNumber)

	if forks.EIP155 {
		signer = crypto.NewEIP155Signer(uint64(s.blockchain.Config().Params.ChainID))
	} else {
		signer = &crypto.FrontierSigner{}
	}

	return signer
}
