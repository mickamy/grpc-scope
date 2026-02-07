package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mickamy/grpc-scope/tui"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "monitor":
		runMonitor()
	case "version":
		fmt.Printf("grpc-scope %s\n", version)
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runMonitor() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: grpc-scope monitor <host:port>")
		os.Exit(1)
	}

	target := os.Args[2]
	m := tui.NewModel(target)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "grpc-scope - gRPC/ConnectRPC development TUI tool")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Usage: grpc-scope <command> [args]\n\n")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  monitor <host:port>   Watch gRPC traffic in real-time")
	fmt.Fprintln(os.Stderr, "  version               Print version")
}
