module github.com/mickamy/grpc-scope/examples/connect

go 1.24.0

require (
	connectrpc.com/connect v1.19.1
	github.com/mickamy/grpc-scope/cinterceptor v0.0.0
	github.com/mickamy/grpc-scope/scope v0.0.0
	golang.org/x/net v0.47.0
	google.golang.org/grpc v1.78.0
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251029180050-ab9386a59fda // indirect
)

replace (
	github.com/mickamy/grpc-scope/cinterceptor => ../../cinterceptor
	github.com/mickamy/grpc-scope/scope => ../../scope
)
