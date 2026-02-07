package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/mickamy/grpc-scope/cinterceptor"

	greeterv1 "github.com/mickamy/grpc-scope/examples/connect/gen/greeter/v1"
	"github.com/mickamy/grpc-scope/examples/connect/gen/greeter/v1/greeterv1connect"
)

type greeterServer struct {
	greeterv1connect.UnimplementedGreeterServiceHandler
}

func (s *greeterServer) SayHello(_ context.Context, req *connect.Request[greeterv1.SayHelloRequest]) (*connect.Response[greeterv1.SayHelloResponse], error) {
	return connect.NewResponse(&greeterv1.SayHelloResponse{
		Message: "Hello, " + req.Msg.GetName() + "!",
	}), nil
}

func main() {
	scope, err := cinterceptor.New(cinterceptor.WithPort(9090))
	if err != nil {
		log.Fatal(err)
	}
	defer scope.Close()

	mux := http.NewServeMux()
	path, handler := greeterv1connect.NewGreeterServiceHandler(
		&greeterServer{},
		connect.WithInterceptors(scope.Interceptor()),
	)
	mux.Handle(path, handler)

	fmt.Println("Connect server listening on :8080 (scope on :9090)")
	if err := http.ListenAndServe(":8080", h2c.NewHandler(mux, &http2.Server{})); err != nil {
		log.Fatal(err)
	}
}
