package rpc

import (
	"encoding/json"
	"fmt"
	"github.com/bytedance/sonic"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"log"
)

func (s *RpcServer) WebsocketStart() error {
	svc := fiber.New(fiber.Config{
		Prefork:               false,
		ServerHeader:          "doge syncer team",
		DisableStartupMessage: true,
		JSONEncoder:           sonic.Marshal,
		JSONDecoder:           sonic.Unmarshal,
	})
	svc.Use("*", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	svc.Get("/wss/*", websocket.New(s.handle))

	s.logger.Info("ws", "address", s.addr, "port", s.port)
	if err := svc.Listen(":3000"); err != nil {
		s.logger.Error("start websocket failed")
	}
	return nil
}

// isSupportedWSType returns a status indicating if the message type is supported
func isSupportedWSType(messageType int) bool {
	return messageType == websocket.TextMessage ||
		messageType == websocket.BinaryMessage
}

func (s *RpcServer) handle(c *websocket.Conn) {
	for {
		msgType, message, err := c.ReadMessage()
		if err != nil {
			log.Println(fmt.Sprintf("Unable to read WS message, %s", err.Error()))
			break
		}

		go func() {
			resp, handleErr := s.handleWs(message)
			if handleErr != nil {
				log.Println(fmt.Sprintf("Unable to handle WS request, %s", handleErr.Error()))

				_ = c.WriteMessage(
					msgType,
					[]byte(fmt.Sprintf("WS Handle error: %s", handleErr.Error())),
				)
			} else {
				_ = c.WriteMessage(msgType, resp)
			}
		}()
	}
}

func (s *RpcServer) handleWs(reqBody []byte) ([]byte, error) {
	var req Request
	if err := json.Unmarshal(reqBody, &req); err != nil {
		return NewRPCResponse(req.ID, "2.0", nil, NewInvalidRequestError("Invalid json request")).Bytes()
	}

	// if the request method is eth_subscribe we need to create a
	// new filter with ws connection
	if req.Method == "eth_subscribe" {

	}
	if req.Method == "eth_unsubscribe" {
	}
	resp, err := s.handleReq(req)
	if err != nil {
		return nil, err
	}

	return NewRPCResponse(req.ID, "2.0", resp, err).Bytes()
}

func (s *RpcServer) handleReq(req Request) ([]byte, Error) {
	rpcFunc, ok := s.routers[req.Method]
	if !ok {
		return nil, NewInternalError("Internal error")
	}
	res := rpcFunc(req.Method, req.Params)
	data, err := json.Marshal(res)
	if err != nil {
		return nil, NewInternalError("Internal error")
	}
	return data, nil
}
