package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/mickamy/grpc-scope/cinterceptor"

	greeterv1 "github.com/mickamy/grpc-scope/examples/connect/gen/greeter/v1"
	"github.com/mickamy/grpc-scope/examples/connect/gen/greeter/v1/greeterv1connect"
	todov1 "github.com/mickamy/grpc-scope/examples/connect/gen/todo/v1"
	"github.com/mickamy/grpc-scope/examples/connect/gen/todo/v1/todov1connect"
)

type greeterServer struct {
	greeterv1connect.UnimplementedGreeterServiceHandler
}

func (s *greeterServer) SayHello(_ context.Context, req *connect.Request[greeterv1.SayHelloRequest]) (*connect.Response[greeterv1.SayHelloResponse], error) {
	return connect.NewResponse(&greeterv1.SayHelloResponse{
		Message: "Hello, " + req.Msg.GetName() + "!",
	}), nil
}

type todoServer struct {
	todov1connect.UnimplementedTodoServiceHandler
	mu      sync.Mutex
	todos   map[string]*todov1.Todo
	counter int
}

func (s *todoServer) CreateTodo(_ context.Context, req *connect.Request[todov1.CreateTodoRequest]) (*connect.Response[todov1.Todo], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.counter++
	todo := &todov1.Todo{
		Id:          fmt.Sprintf("todo-%d", s.counter),
		Title:       req.Msg.GetTitle(),
		Description: req.Msg.GetDescription(),
		Priority:    req.Msg.GetPriority(),
		Tags:        req.Msg.GetTags(),
		CreatedAt:   timestamppb.Now(),
	}
	s.todos[todo.Id] = todo
	return connect.NewResponse(todo), nil
}

func (s *todoServer) GetTodo(_ context.Context, req *connect.Request[todov1.GetTodoRequest]) (*connect.Response[todov1.Todo], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	todo, ok := s.todos[req.Msg.GetId()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("todo %q not found", req.Msg.GetId()))
	}
	return connect.NewResponse(todo), nil
}

func (s *todoServer) ListTodos(_ context.Context, req *connect.Request[todov1.ListTodosRequest]) (*connect.Response[todov1.ListTodosResponse], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var todos []*todov1.Todo
	for _, t := range s.todos {
		if req.Msg.GetCompletedOnly() && !t.Completed {
			continue
		}
		todos = append(todos, t)
		if req.Msg.GetLimit() > 0 && int32(len(todos)) >= req.Msg.GetLimit() {
			break
		}
	}
	return connect.NewResponse(&todov1.ListTodosResponse{
		Todos:      todos,
		TotalCount: int32(len(todos)),
	}), nil
}

func (s *todoServer) UpdateTodo(_ context.Context, req *connect.Request[todov1.UpdateTodoRequest]) (*connect.Response[todov1.Todo], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	todo, ok := s.todos[req.Msg.GetId()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("todo %q not found", req.Msg.GetId()))
	}
	if req.Msg.GetTitle() != "" {
		todo.Title = req.Msg.GetTitle()
	}
	todo.Completed = req.Msg.GetCompleted()
	return connect.NewResponse(todo), nil
}

func (s *todoServer) DeleteTodo(_ context.Context, req *connect.Request[todov1.DeleteTodoRequest]) (*connect.Response[todov1.DeleteTodoResponse], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.todos[req.Msg.GetId()]; !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("todo %q not found", req.Msg.GetId()))
	}
	delete(s.todos, req.Msg.GetId())
	return connect.NewResponse(&todov1.DeleteTodoResponse{}), nil
}

func main() {
	scope, err := cinterceptor.New(cinterceptor.WithPort(9090))
	if err != nil {
		log.Fatal(err)
	}
	defer scope.Close()

	mux := http.NewServeMux()
	interceptors := connect.WithInterceptors(scope.Interceptor())

	path, handler := greeterv1connect.NewGreeterServiceHandler(&greeterServer{}, interceptors)
	mux.Handle(path, handler)

	todoPath, todoHandler := todov1connect.NewTodoServiceHandler(
		&todoServer{todos: make(map[string]*todov1.Todo)},
		interceptors,
	)
	mux.Handle(todoPath, todoHandler)

	fmt.Println("Connect server listening on :8080 (scope on :9090)")
	if err := http.ListenAndServe(":8080", h2c.NewHandler(mux, &http2.Server{})); err != nil {
		log.Fatal(err)
	}
}
