package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/mickamy/grpc-scope/ginterceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	greeterv1 "github.com/mickamy/grpc-scope/examples/grpc/gen/greeter/v1"
)

type greeterServer struct {
	greeterv1.UnimplementedGreeterServiceServer
}

func (s *greeterServer) SayHello(_ context.Context, req *greeterv1.SayHelloRequest) (*greeterv1.SayHelloResponse, error) {
	return &greeterv1.SayHelloResponse{
		Message: "Hello, " + req.GetName() + "!",
	}, nil
}

func main() {
	scope, err := ginterceptor.New(ginterceptor.WithPort(9090))
	if err != nil {
		log.Fatal(err)
	}
	defer scope.Close()

	srv := grpc.NewServer(
		grpc.UnaryInterceptor(scope.UnaryInterceptor()),
		grpc.StreamInterceptor(scope.StreamInterceptor()),
	)
	greeterv1.RegisterGreeterServiceServer(srv, &greeterServer{})
	reflection.Register(srv)

	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("gRPC server listening on :8080 (scope on :9090)")
	if err := srv.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
