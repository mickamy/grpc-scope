# Examples

Example servers demonstrating grpc-scope with gRPC and ConnectRPC.

Both examples register two services — **GreeterService** and **TodoService** — and expose the scope server on port
`9090`.

## Prerequisites

```bash
go install github.com/mickamy/grpc-scope@latest
```

## gRPC

### Run the server

```bash
cd examples/grpc
go run main.go
# gRPC server listening on :8080 (scope on :9090)
```

### Start the monitor

```bash
grpc-scope monitor localhost:9090 localhost:8080
```

### Send requests with grpcurl

```bash
# SayHello
grpcurl -plaintext -d '{"name": "World"}' \
  localhost:8080 greeter.v1.GreeterService/SayHello

# CreateTodo
grpcurl -plaintext -d '{"title": "Buy milk", "description": "From the store", "priority": "PRIORITY_HIGH", "tags": ["shopping"]}' \
  localhost:8080 todo.v1.TodoService/CreateTodo

# GetTodo
grpcurl -plaintext -d '{"id": "todo-1"}' \
  localhost:8080 todo.v1.TodoService/GetTodo

# ListTodos
grpcurl -plaintext -d '{"limit": 10}' \
  localhost:8080 todo.v1.TodoService/ListTodos

# UpdateTodo
grpcurl -plaintext -d '{"id": "todo-1", "title": "Buy oat milk", "completed": true}' \
  localhost:8080 todo.v1.TodoService/UpdateTodo

# DeleteTodo
grpcurl -plaintext -d '{"id": "todo-1"}' \
  localhost:8080 todo.v1.TodoService/DeleteTodo

# List available services
grpcurl -plaintext localhost:8080 list
```

## ConnectRPC

### Run the server

```bash
cd examples/connect
go run main.go
# Connect server listening on :8080 (scope on :9090)
```

### Start the monitor

```bash
grpc-scope monitor localhost:9090 localhost:8080
```

### Send requests with curl

```bash
# SayHello
curl -X POST http://localhost:8080/greeter.v1.GreeterService/SayHello \
  -H 'Content-Type: application/json' \
  -d '{"name": "World"}'

# CreateTodo
curl -X POST http://localhost:8080/todo.v1.TodoService/CreateTodo \
  -H 'Content-Type: application/json' \
  -d '{"title": "Buy milk", "description": "From the store", "priority": "PRIORITY_HIGH", "tags": ["shopping"]}'

# GetTodo
curl -X POST http://localhost:8080/todo.v1.TodoService/GetTodo \
  -H 'Content-Type: application/json' \
  -d '{"id": "todo-1"}'

# ListTodos
curl -X POST http://localhost:8080/todo.v1.TodoService/ListTodos \
  -H 'Content-Type: application/json' \
  -d '{"limit": 10}'

# UpdateTodo
curl -X POST http://localhost:8080/todo.v1.TodoService/UpdateTodo \
  -H 'Content-Type: application/json' \
  -d '{"id": "todo-1", "title": "Buy oat milk", "completed": true}'

# DeleteTodo
curl -X POST http://localhost:8080/todo.v1.TodoService/DeleteTodo \
  -H 'Content-Type: application/json' \
  -d '{"id": "todo-1"}'
```
