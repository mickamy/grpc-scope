# grpc-scope

Real-time gRPC & ConnectRPC traffic monitor and replay TUI.

## Features

- **Real-time monitoring** — watch gRPC/ConnectRPC calls as they happen
- **Request & response inspection** — view full payloads with pretty-printed JSON
- **Replay** — resend a captured request to your application server
- **Edit & replay** — open request payloads in `$EDITOR`, modify, and resend
- **gRPC + ConnectRPC support** — drop-in interceptors for both frameworks
- **Metadata capture** — inspect headers and trailers alongside payloads

## Installation

### CLI

```bash
go install github.com/mickamy/grpc-scope@latest
```

### Interceptor library

Add the interceptor package to your application:

```bash
# For gRPC
go get github.com/mickamy/grpc-scope/ginterceptor

# For ConnectRPC
go get github.com/mickamy/grpc-scope/cinterceptor
```

## Quick Start

### gRPC

Add the interceptor to your server:

```go
package main

import (
	"log"
	"net"

	"github.com/mickamy/grpc-scope/ginterceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

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
	// Register your services...
	reflection.Register(srv)

	lis, _ := net.Listen("tcp", ":8080")
	srv.Serve(lis)
}
```

Then run the monitor:

```bash
grpc-scope monitor localhost:9090 localhost:8080
```

### ConnectRPC

Add the interceptor to your server:

```go
package main

import (
	"log"
	"net/http"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/mickamy/grpc-scope/cinterceptor"
)

func main() {
	scope, err := cinterceptor.New(cinterceptor.WithPort(9090))
	if err != nil {
		log.Fatal(err)
	}
	defer scope.Close()

	mux := http.NewServeMux()
	interceptors := connect.WithInterceptors(scope.Interceptor())

	// Register your services with the interceptor...
	// path, handler := somev1connect.NewSomeServiceHandler(&server{}, interceptors)
	// mux.Handle(path, handler)

	http.ListenAndServe(":8080", h2c.NewHandler(mux, &http2.Server{}))
}
```

Then run the monitor:

```bash
grpc-scope monitor localhost:9090 localhost:8080
```

## Usage

```
grpc-scope monitor <scope-addr> [app-addr]
grpc-scope version
grpc-scope help
```

- `<scope-addr>` — address of the scope server started by the interceptor (e.g. `localhost:9090`)
- `[app-addr]` — application server address; providing this enables replay (`r` / `e` keys)

## Keybindings

| Key            | Action                          |
|----------------|---------------------------------|
| `j` / `Down`   | Move down                       |
| `k` / `Up`     | Move up                         |
| `r`            | Replay selected request         |
| `e`            | Edit in `$EDITOR` and replay    |
| `q` / `Ctrl+C` | Quit (or back from replay view) |

> `r` and `e` are only available when `app-addr` is provided.

## Architecture

1. An interceptor (`ginterceptor` or `cinterceptor`) wraps your server and captures every RPC call.
2. The interceptor runs an internal gRPC server (default port `9090`) that publishes captured calls.
3. The `grpc-scope monitor` TUI connects to this server via a `Watch` stream and displays calls in real time.
4. Replay uses gRPC reflection on the application server to resend requests.

## License

[MIT](./LICENSE)
