package rpc

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-hclog"
	"github.com/sunvim/dogesyncer/blockchain"
)

type RpcServer struct {
	logger     hclog.Logger
	ctx        context.Context
	blockchain *blockchain.Blockchain
	addr       string
	port       string
	routers    map[string]RpcFunc
}

func NewRpcServer(logger hclog.Logger,
	blockchain *blockchain.Blockchain,
	addr, port string) *RpcServer {
	s := &RpcServer{
		logger:     logger.Named("rpc"),
		addr:       addr,
		port:       port,
		blockchain: blockchain,
	}
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
				c.Status(fiber.StatusBadRequest).SendString("error request")
				return nil
			}

			rsp := resPool.Get().(*Response)
			defer resPool.Put(rsp)

			exeMethod, ok := s.routers[req.Method]
			if !ok {
				s.logger.Error("route", "not support method", req.Method)
				c.Status(fiber.StatusBadRequest).SendString("not support method")
				return nil
			}

			rsp.Result = exeMethod(req.Method, req.Params)
			rsp.ID = req.ID
			rsp.Version = req.Version

			c.Status(fiber.StatusOK).JSON(rsp)

			return nil
		})

		svc.Listen(ap)
	}(ctx)

	return nil
}

func (s *RpcServer) initmethods() {
	s.routers = map[string]RpcFunc{
		"eth_blockNumber": s.GetBlockNumber,
		"eth_getBalance":  s.GetBalance,
	}
}
