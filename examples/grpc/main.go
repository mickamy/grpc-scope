package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/mickamy/grpc-scope/ginterceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	greeterv1 "github.com/mickamy/grpc-scope/examples/grpc/gen/greeter/v1"
	todov1 "github.com/mickamy/grpc-scope/examples/grpc/gen/todo/v1"
)

type greeterServer struct {
	greeterv1.UnimplementedGreeterServiceServer
}

func (s *greeterServer) SayHello(_ context.Context, req *greeterv1.SayHelloRequest) (*greeterv1.SayHelloResponse, error) {
	return &greeterv1.SayHelloResponse{
		Message: "Hello, " + req.GetName() + "!",
	}, nil
}

type todoServer struct {
	todov1.UnimplementedTodoServiceServer
	mu      sync.Mutex
	todos   map[string]*todov1.Todo
	counter int
}

func (s *todoServer) CreateTodo(_ context.Context, req *todov1.CreateTodoRequest) (*todov1.Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.counter++
	todo := &todov1.Todo{
		Id:          fmt.Sprintf("todo-%d", s.counter),
		Title:       req.GetTitle(),
		Description: req.GetDescription(),
		Priority:    req.GetPriority(),
		Tags:        req.GetTags(),
		CreatedAt:   timestamppb.Now(),
	}
	s.todos[todo.Id] = todo
	return todo, nil
}

func (s *todoServer) GetTodo(_ context.Context, req *todov1.GetTodoRequest) (*todov1.Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	todo, ok := s.todos[req.GetId()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "todo %q not found", req.GetId())
	}
	return todo, nil
}

func (s *todoServer) ListTodos(_ context.Context, req *todov1.ListTodosRequest) (*todov1.ListTodosResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var todos []*todov1.Todo
	for _, t := range s.todos {
		if req.GetCompletedOnly() && !t.Completed {
			continue
		}
		todos = append(todos, t)
		if req.GetLimit() > 0 && int32(len(todos)) >= req.GetLimit() {
			break
		}
	}
	return &todov1.ListTodosResponse{
		Todos:      todos,
		TotalCount: int32(len(todos)),
	}, nil
}

func (s *todoServer) UpdateTodo(_ context.Context, req *todov1.UpdateTodoRequest) (*todov1.Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	todo, ok := s.todos[req.GetId()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "todo %q not found", req.GetId())
	}
	if req.GetTitle() != "" {
		todo.Title = req.GetTitle()
	}
	todo.Completed = req.GetCompleted()
	return todo, nil
}

func (s *todoServer) DeleteTodo(_ context.Context, req *todov1.DeleteTodoRequest) (*todov1.DeleteTodoResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.todos[req.GetId()]; !ok {
		return nil, status.Errorf(codes.NotFound, "todo %q not found", req.GetId())
	}
	delete(s.todos, req.GetId())
	return &todov1.DeleteTodoResponse{}, nil
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
	todov1.RegisterTodoServiceServer(srv, &todoServer{todos: make(map[string]*todov1.Todo)})
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
