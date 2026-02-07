package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mickamy/grpc-scope/scope/domain"
	scopev1 "github.com/mickamy/grpc-scope/scope/gen/scope/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// EventMsg is sent when a new call event is received from the Watch stream.
type EventMsg struct {
	Event  *scopev1.CallEvent
	stream scopev1.ScopeService_WatchClient
}

// ErrMsg is sent when the Watch stream encounters an error.
type ErrMsg struct {
	Err error
}

// connectedMsg is sent after successfully connecting to the scope server.
type connectedMsg struct {
	stream scopev1.ScopeService_WatchClient
	conn   *grpc.ClientConn
}

// Model is the Bubbletea model for the monitor TUI.
type Model struct {
	target string
	events []*scopev1.CallEvent
	cursor int
	width  int
	height int
	err    error
	conn   *grpc.ClientConn
	cancel context.CancelFunc
}

// NewModel creates a new TUI model that connects to the given target address.
func NewModel(target string) Model {
	return Model{
		target: target,
	}
}

func (m Model) Init() tea.Cmd {
	return m.connect()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.cleanup()
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.events)-1 {
				m.cursor++
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case connectedMsg:
		m.conn = msg.conn
		return m, recvEvent(msg.stream)
	case EventMsg:
		m.events = append(m.events, msg.Event)
		return m, recvEvent(msg.stream)
	case ErrMsg:
		m.err = msg.Err
	}
	return m, nil
}

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("%s\nPress q to quit.", friendlyError(m.target, m.err))
	}

	if m.width == 0 {
		return "Connecting..."
	}

	listHeight := m.height/2 - 2
	if listHeight < 3 {
		listHeight = 3
	}

	list := m.renderList(listHeight)
	detail := m.renderDetail()

	return lipgloss.JoinVertical(lipgloss.Left, list, detail)
}

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	borderStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	labelStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
)

func (m Model) renderList(maxRows int) string {
	header := fmt.Sprintf("  %-40s %-12s %-10s %s", "Method", "Status", "Latency", "Time")
	lines := []string{headerStyle.Render(header)}

	start := 0
	if len(m.events) > maxRows {
		start = len(m.events) - maxRows
		if m.cursor < start {
			start = m.cursor
		}
	}

	end := start + maxRows
	if end > len(m.events) {
		end = len(m.events)
	}

	for i := start; i < end; i++ {
		ev := m.events[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "▶ "
		}

		statusStr := domain.StatusCode(ev.GetStatusCode()).String()
		latency := ""
		if ev.GetDuration() != nil {
			latency = ev.GetDuration().AsDuration().String()
		}
		timeStr := ""
		if ev.GetStartTime() != nil {
			timeStr = ev.GetStartTime().AsTime().Local().Format("15:04:05")
		}

		line := fmt.Sprintf("%s%-40s %-12s %-10s %s",
			cursor,
			truncate(ev.GetMethod(), 40),
			statusStr,
			latency,
			timeStr,
		)

		if i == m.cursor {
			line = selectedStyle.Render(line)
		} else if domain.StatusCode(ev.GetStatusCode()) != domain.StatusOK {
			line = errorStyle.Render(line)
		}

		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	title := fmt.Sprintf(" gRPC Traffic (%d events) ", len(m.events))
	return borderStyle.Width(m.width - 2).Render(title + "\n" + content)
}

func (m Model) renderDetail() string {
	if len(m.events) == 0 {
		return borderStyle.Width(m.width - 2).Render(" Detail \nNo events yet.")
	}

	ev := m.events[m.cursor]

	var b strings.Builder
	b.WriteString(labelStyle.Render("Method: "))
	b.WriteString(ev.GetMethod())
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Status: "))
	b.WriteString(fmt.Sprintf("%s (%s)", domain.StatusCode(ev.GetStatusCode()).String(), ev.GetStatusMessage()))
	b.WriteString("\n")

	if ev.GetDuration() != nil {
		b.WriteString(labelStyle.Render("Latency: "))
		b.WriteString(ev.GetDuration().AsDuration().String())
		b.WriteString("\n")
	}

	if ev.GetRequestPayload() != "" {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("Request:"))
		b.WriteString("\n")
		b.WriteString(ev.GetRequestPayload())
		b.WriteString("\n")
	}

	if ev.GetResponsePayload() != "" {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("Response:"))
		b.WriteString("\n")
		b.WriteString(ev.GetResponsePayload())
	}

	return borderStyle.Width(m.width - 2).Render(" Detail \n" + b.String())
}

func (m Model) connect() tea.Cmd {
	return func() tea.Msg {
		conn, err := grpc.NewClient(
			m.target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("failed to connect: %w", err)}
		}

		client := scopev1.NewScopeServiceClient(conn)
		stream, err := client.Watch(context.Background(), &scopev1.WatchRequest{})
		if err != nil {
			conn.Close()
			return ErrMsg{Err: fmt.Errorf("failed to start watch: %w", err)}
		}

		return connectedMsg{stream: stream, conn: conn}
	}
}

func recvEvent(stream scopev1.ScopeService_WatchClient) tea.Cmd {
	return func() tea.Msg {
		resp, err := stream.Recv()
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("watch stream error: %w", err)}
		}
		return EventMsg{Event: resp.GetEvent(), stream: stream}
	}
}

func (m *Model) cleanup() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.conn != nil {
		_ = m.conn.Close()
	}
}

func friendlyError(target string, err error) string {
	st, ok := status.FromError(err)
	if ok {
		switch st.Code() {
		case codes.Unavailable:
			return fmt.Sprintf(
				"Could not connect to %s\n\n"+
					"Make sure the interceptor is running in your gRPC server:\n\n"+
					"  scope := interceptor.New(interceptor.WithPort(...))\n"+
					"  grpc.NewServer(\n"+
					"    grpc.UnaryInterceptor(scope.UnaryInterceptor()),\n"+
					"  )",
				target,
			)
		case codes.Unimplemented:
			return fmt.Sprintf(
				"Connected to %s, but ScopeService is not available.\n\n"+
					"The server does not have the grpc-scope interceptor installed.\n"+
					"Make sure you are connecting to the interceptor port, not your app port.",
				target,
			)
		}
	}

	if strings.Contains(err.Error(), "connection refused") {
		return fmt.Sprintf(
			"Connection refused: %s\n\n"+
				"Is the interceptor running on this address?",
			target,
		)
	}

	return fmt.Sprintf("Error: %v", err)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
