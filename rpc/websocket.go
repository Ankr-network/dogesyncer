package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bytedance/sonic"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/hashicorp/go-hclog"
	"math"
	"sync"
)

func (s *RpcServer) WebsocketStart(ctx context.Context) error {
	go func(ctx context.Context) {
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

		svc.Get("/ws/*", websocket.New(s.handle))

		addr := fmt.Sprintf("%s:%s", s.websocketAddr, s.websocketPort)
		s.logger.Info("websocket", "addr", addr)
		if err := svc.Listen(addr); err != nil {
			s.logger.Error("start websocket failed", "err", err)
			return
		}
		s.logger.Info("websocket start success.")
		return
	}(ctx)
	return nil
}

// wsWrapper is a wrapping object for the web socket connection and logger
type wsWrapper struct {
	sync.Mutex // basic r/w lock

	ws       *websocket.Conn // the actual WS connection
	logger   hclog.Logger    // module logger
	filterID string          // filter ID
}

func (w *wsWrapper) SetFilterID(filterID string) {
	w.filterID = filterID
}

func (w *wsWrapper) GetFilterID() string {
	return w.filterID
}

// WriteMessage writes out the message to the WS peer
func (w *wsWrapper) WriteMessage(messageType int, data []byte) error {
	w.Lock()
	defer w.Unlock()
	writeErr := w.ws.WriteMessage(messageType, data)

	if writeErr != nil {
		w.logger.Error(
			fmt.Sprintf("Unable to write WS message, %s", writeErr.Error()),
		)
	}

	return writeErr
}

// isSupportedWSType returns a status indicating if the message type is supported
func isSupportedWSType(messageType int) bool {
	return messageType == websocket.TextMessage ||
		messageType == websocket.BinaryMessage
}

func (s *RpcServer) handle(c *websocket.Conn) {
	wrapConn := &wsWrapper{ws: c, logger: s.logger}

	for {
		msgType, message, err := c.ReadMessage()
		if err != nil {
			s.logger.Error(fmt.Sprintf("Unable to read WS message, %s", err.Error()))
			s.filterManager.RemoveFilterByWs(wrapConn)
			break
		}
		if isSupportedWSType(msgType) {
			go func() {
				resp, handleErr := s.handleWs(message, wrapConn)
				if handleErr != nil {
					s.logger.Error(fmt.Sprintf("Unable to handle WS request, %s", handleErr.Error()))

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
}

func (s *RpcServer) handleWs(reqBody []byte, conn wsConn) ([]byte, error) {
	var req Request
	if err := json.Unmarshal(reqBody, &req); err != nil {
		return NewRPCResponse(req.ID, "2.0", nil, NewInvalidRequestError("Invalid json request")).Bytes()
	}

	// if the request method is eth_subscribe we need to create a
	// new filter with ws connection
	if req.Method == "eth_subscribe" {
		filterID, err := s.handleSubscribe(req, conn)
		if err != nil {
			s.logger.Error("handleSubscribe error.", "err", err)
			return NewRPCResponse(req.ID, "2.0", nil, err).Bytes()
		}

		resp, err := formatFilterResponse(req.ID, filterID)
		if err != nil {
			s.logger.Error("formatFilterResponse error", "err", err)
			return NewRPCResponse(req.ID, "2.0", nil, err).Bytes()
		}

		return []byte(resp), nil
	}
	if req.Method == "eth_unsubscribe" {
		ok, err := s.handleUnsubscribe(req)
		if err != nil {
			return nil, err
		}

		res := "false"
		if ok {
			res = "true"
		}

		resp, err := formatFilterResponse(req.ID, res)
		if err != nil {
			return NewRPCResponse(req.ID, "2.0", nil, err).Bytes()
		}

		return []byte(resp), nil
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
	res, err := rpcFunc(req.Method, req.Params)
	if err != nil {
		return nil, err
	}
	data, err2 := json.Marshal(res)
	if err2 != nil {
		return nil, NewInternalError("Internal error")
	}
	return data, nil
}

func (s *RpcServer) handleSubscribe(req Request, conn wsConn) (string, Error) {
	params, err := GetPrams(req.Params)
	if err != nil {
		return "", NewInvalidRequestError("Invalid json request")
	}

	if len(params) < 1 {
		return "", NewInvalidParamsError("Invalid params")
	}

	subscribeMethod, ok := params[0].(string)
	if !ok {
		return "", NewSubscriptionNotFoundError(subscribeMethod)
	}

	var filterID string
	if subscribeMethod == "newHeads" {
		filterID = s.filterManager.NewBlockFilter(conn)
	} else if subscribeMethod == "logs" {
		logQuery, err := decodeLogQueryFromInterface(params[1])
		if err != nil {
			return "", NewInternalError(err.Error())
		}
		filterID = s.filterManager.NewLogFilter(logQuery, conn)
	} else {
		return "", NewSubscriptionNotFoundError(subscribeMethod)
	}

	return filterID, nil
}

func formatFilterResponse(id interface{}, resp string) (string, Error) {
	switch t := id.(type) {
	case string:
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","result":"%s"}`, t, resp), nil
	case float64:
		if t == math.Trunc(t) {
			return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":"%s"}`, int(t), resp), nil
		} else {
			return "", NewInvalidRequestError("Invalid json request")
		}
	case nil:
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":null,"result":"%s"}`, resp), nil
	default:
		return "", NewInvalidRequestError("Invalid json request")
	}
}

func (s *RpcServer) handleUnsubscribe(req Request) (bool, Error) {
	params, err := GetPrams(req.Params)
	if err != nil {
		return false, NewInvalidRequestError("Invalid json request")
	}

	if len(params) != 1 {
		return false, NewInvalidParamsError("Invalid params")
	}

	filterID, ok := params[0].(string)
	if !ok {
		return false, NewSubscriptionNotFoundError(filterID)
	}

	return s.filterManager.Uninstall(filterID), nil
}
