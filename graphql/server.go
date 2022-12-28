package graphql

import (
	"encoding/json"
	"fmt"
	"github.com/ankr/dogesyncer/blockchain"
	"github.com/graph-gophers/graphql-go"
	_ "github.com/graph-gophers/graphql-go"
	"github.com/hashicorp/go-hclog"
	"net"
	"net/http"
	"time"
)

//var graphqlSchema *graphql.Schema
//
//const graphQLDepth = 10

//const ( // local service
//	EthBlockNumber = "eth_blockNumber"
//)
//
//var (
//	HandlerMap = map[string]rpc.RpcFunc{
//		EthBlockNumber: blockNumber,
//	}
//)

//type GraphServer struct {
//	logger     hclog.Logger
//	blockchain *blockchain.Blockchain
//	addr       string
//	port       string
//	handlerMap map[string]rpc.RpcFunc
//}

type GraphQLService struct {
	logger     hclog.Logger
	config     *Config
	ui         *GraphiQL
	handler    *handler
	blockchain *blockchain.Blockchain
}

type Config struct {
	Store GraphQLStore
	Addr  *net.TCPAddr
	//Forks                    chain.Forks
	ChainID                  uint64
	AccessControlAllowOrigin []string
	BlockRangeLimit          uint64
	EnablePProf              bool
}

// GraphQLStore defines all the methods required
// by all the JSON RPC endpoints
type GraphQLStore interface {
	ethStore
	txPoolStore
	filterManagerStore
}

type handler struct {
	Schema *graphql.Schema
}

func NewGraphQLService(logger hclog.Logger, blockchain *blockchain.Blockchain, config *Config) error {
	q := Resolver{
		backend: config.Store,
		//chainID: config.ChainID,
		//filterManager: rpc.NewFilterManager(hclog.NewNullLogger(), config.Store, config.BlockRangeLimit),
	}

	s, err := graphql.ParseSchema(schema, &q)
	if err != nil {
		return err
	}

	srv := &GraphQLService{
		logger:     logger.Named("graphql"),
		config:     config,
		ui:         &GraphiQL{},
		handler:    &handler{Schema: s},
		blockchain: blockchain,
	}

	// start http server
	if err := srv.setupHTTP(); err != nil {
		return err
	}

	return nil
}

func (svc *GraphQLService) setupHTTP() error {
	svc.logger.Info("graphql server started", "addr", svc.config.Addr.String())

	lis, err := net.Listen("tcp", svc.config.Addr.String())
	if err != nil {
		return err
	}

	//var mux *http.ServeMux
	//if svc.config.EnablePProf {
	//	// debug feature enabled
	//	mux = http.DefaultServeMux
	//} else {
	//	// NewServeMux must be used, as it disables all debug features.
	//	// For some strange reason, with DefaultServeMux debug/vars is always enabled (but not debug/pprof).
	//	// If pprof need to be enabled, this should be DefaultServeMux
	//	mux = http.NewServeMux()
	//}
	mux := http.NewServeMux()

	// The middleware factory returns a handler, so we need to wrap the handler function properly.
	graphqlHandler := http.HandlerFunc(svc.handler.ServeHTTP)
	mux.Handle("/graphql/ui", middlewareFactory(svc.config)(http.HandlerFunc(svc.ui.ServeHTTP)))
	mux.Handle("/graphql", middlewareFactory(svc.config)(graphqlHandler))
	mux.Handle("/graphql/", middlewareFactory(svc.config)(graphqlHandler))

	srv := http.Server{
		Handler:           mux,
		ReadHeaderTimeout: time.Minute,
	}

	go func() {
		if err := srv.Serve(lis); err != nil {
			svc.logger.Error("closed http connection", "err", err)
		}
	}()

	return nil
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var params struct {
		Query         string                 `json:"query"`
		OperationName string                 `json:"operationName"`
		Variables     map[string]interface{} `json:"variables"`
	}

	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	// exec schema query
	response := h.Schema.Exec(r.Context(), params.Query, params.OperationName, params.Variables)

	// marshal response
	responseJSON, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	if len(response.Errors) > 0 {
		w.WriteHeader(http.StatusBadRequest)
	}

	w.Header().Set("Content-Type", "application/json")

	_, err = w.Write(responseJSON)
	if err != nil {
		respond(w, errorJSON(fmt.Sprintf("graphql response write failed: %v", err)), http.StatusBadRequest)
	}
}

// The middlewareFactory builds a middleware which enables CORS using the provided config.
func middlewareFactory(config *Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			for _, allowedOrigin := range config.AccessControlAllowOrigin {
				if allowedOrigin == "*" {
					w.Header().Set("Access-Control-Allow-Origin", "*")

					break
				}

				if allowedOrigin == origin {
					w.Header().Set("Access-Control-Allow-Origin", origin)

					break
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

//func NewGraphServer(logger hclog.Logger, blockchain *blockchain.Blockchain, addr, port string) *GraphServer {
//	g := &GraphServer{
//		logger:     logger.Named("graphql"),
//		blockchain: blockchain,
//		addr:       addr,
//		port:       port,
//	}
//	g.initMethods()
//	return g
//}
//
//func (g *GraphServer) initMethods() {
//	g.handlerMap = map[string]rpc.RpcFunc{
//		"eth_blockNumber": g.blockNumber,
//	}
//}
//
//// Resolver is the top-level object in the GraphQL hierarchy.
//type Resolver struct {
//	//backendHandler map[string]func(rpc.Request, interface{}) []byte
//	backendHandler map[string]rpc.RpcFunc
//}
//
//func (g *GraphServer) RegisterHandler() error {
//	q := Resolver{
//		backendHandler: g.handlerMap,
//	}
//
//	s, err := graphql.ParseSchema(schema, &q, graphql.MaxDepth(graphQLDepth), graphql.MaxParallelism(2*runtime.NumCPU()-1))
//	if err != nil {
//		return err
//	}
//
//	graphqlSchema = s
//
//	svc := fiber.New(fiber.Config{
//		Prefork:               false,
//		ServerHeader:          "doge syncer team",
//		DisableStartupMessage: true,
//		JSONEncoder:           sonic.Marshal,
//		JSONDecoder:           sonic.Unmarshal,
//	})
//
//	svc.Get("/graphql/ui", graphQLUI)
//	svc.Post("/graphql", graphQL)
//
//	return nil
//}
//
//func (g *GraphServer) blockNumber(method string, params ...any) any {
//	num := strconv.FormatInt(int64(g.blockchain.Header().Number), 16)
//	return strings.Join([]string{"0x", num}, "")
//}
