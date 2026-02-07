package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mickamy/grpc-scope/replay"
	"github.com/mickamy/grpc-scope/scope/domain"
	scopev1 "github.com/mickamy/grpc-scope/scope/gen/scope/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type viewMode int

const (
	viewList viewMode = iota
	viewReplay
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

// ReplayResultMsg is sent when a replay call completes.
type ReplayResultMsg struct {
	Result      *replay.Result
	Method      string
	RequestJSON string
	Err         error
}

// EditorFinishedMsg is sent when the $EDITOR exits.
type EditorFinishedMsg struct {
	Payload string
	Event   *scopev1.CallEvent
	Err     error
}

// Model is the Bubbletea model for the monitor TUI.
type Model struct {
	target       string
	appTarget    string // application server address for replay (empty = disabled)
	events       []*scopev1.CallEvent
	cursor       int
	width        int
	height       int
	err          error
	conn         *grpc.ClientConn
	cancel       context.CancelFunc
	mode         viewMode
	replayResult *replayResultView
	replaying    bool
}

type replayResultView struct {
	method      string
	requestJSON string
	result      *replay.Result
	err         error
	scroll      int // scroll offset for viewing long content
	totalLines  int // set during render for scroll bounds
}

// NewModel creates a new TUI model that connects to the given target address.
// appTarget is the application server address for replay; empty disables replay.
func NewModel(target, appTarget string) Model {
	return Model{
		target:    target,
		appTarget: appTarget,
	}
}

func (m Model) Init() tea.Cmd {
	return m.connect()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case connectedMsg:
		m.conn = msg.conn
		return m, recvEvent(msg.stream)
	case EventMsg:
		if !strings.HasPrefix(msg.Event.GetMethod(), "/grpc.reflection.") {
			m.events = append(m.events, nil)
			copy(m.events[1:], m.events)
			m.events[0] = msg.Event
			if len(m.events) > 1 {
				m.cursor++
			}
		}
		return m, recvEvent(msg.stream)
	case ErrMsg:
		m.err = msg.Err
	case ReplayResultMsg:
		m.replaying = false
		m.mode = viewReplay
		m.replayResult = &replayResultView{
			method:      msg.Method,
			requestJSON: msg.RequestJSON,
			result:      msg.Result,
			err:         msg.Err,
		}
	case EditorFinishedMsg:
		if msg.Err != nil {
			m.replaying = false
			m.mode = viewReplay
			m.replayResult = &replayResultView{
				method: msg.Event.GetMethod(),
				err:    msg.Err,
			}
			return m, nil
		}
		return m, m.doReplay(msg.Event, msg.Payload)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.mode == viewReplay {
			m.mode = viewList
			m.replayResult = nil
			return m, nil
		}
		m.cleanup()
		return m, tea.Quit
	case "up", "k":
		return m.navigateUp(), nil
	case "down", "j":
		return m.navigateDown(), nil
	case "r":
		if m.mode == viewReplay && m.appTarget != "" && !m.replaying && m.replayResult != nil {
			m.replaying = true
			ev := m.events[m.cursor]
			return m, m.doReplay(ev, m.replayResult.requestJSON)
		}
		if m.canReplay() {
			m.replaying = true
			ev := m.events[m.cursor]
			return m, m.doReplay(ev, ev.GetRequestPayload())
		}
	case "e":
		if m.canReplay() {
			m.replaying = true
			ev := m.events[m.cursor]
			return m, m.openEditor(ev)
		}
	}
	return m, nil
}

func (m Model) navigateUp() Model {
	if m.mode == viewReplay && m.replayResult != nil && m.replayResult.scroll > 0 {
		m.replayResult.scroll--
	} else if m.mode == viewList && m.cursor > 0 {
		m.cursor--
	}
	return m
}

func (m Model) navigateDown() Model {
	if m.mode == viewReplay && m.replayResult != nil {
		if max := m.replayScrollMax(); m.replayResult.scroll < max {
			m.replayResult.scroll++
		}
	} else if m.mode == viewList && m.cursor < len(m.events)-1 {
		m.cursor++
	}
	return m
}

func (m Model) replayScrollMax() int {
	if m.replayResult == nil {
		return 0
	}
	visibleMax := m.height - 2 - 1
	if visibleMax < 3 {
		visibleMax = 3
	}
	max := m.replayResult.totalLines - visibleMax
	if max < 0 {
		return 0
	}
	return max
}

func (m Model) canReplay() bool {
	return m.appTarget != "" && len(m.events) > 0 && !m.replaying && m.mode == viewList
}

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("%s\nPress q to quit.", friendlyError(m.target, m.err))
	}

	if m.width == 0 {
		return "Connecting..."
	}

	if m.mode == viewReplay {
		return m.renderReplayResult()
	}

	maxListHeight := m.height/3 - 1
	if maxListHeight < 3 {
		maxListHeight = 3
	}
	listHeight := len(m.events)
	if listHeight > maxListHeight {
		listHeight = maxListHeight
	}
	if listHeight < 1 {
		listHeight = 1
	}

	list := m.renderList(listHeight)
	// list panel = border(2) + title(1) + header(1) + rows = listHeight + 4
	// detail panel = border(2) + content
	// help = 1
	detailMaxLines := m.height - (listHeight + 4) - 1 - 2 // 2 for detail border
	if detailMaxLines < 3 {
		detailMaxLines = 3
	}
	detail := m.renderDetail(detailMaxLines)
	help := m.renderHelp()

	return lipgloss.JoinVertical(lipgloss.Left, list, detail, help)
}

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	borderStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	labelStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	helpStyle     = lipgloss.NewStyle().Faint(true)
	successStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
)

func (m Model) methodColumnWidth() int {
	// 2(cursor) + method + 1 + 12(status) + 1 + 10(latency) + 1 + 8(time) + 4(border/padding)
	const fixed = 2 + 1 + 12 + 1 + 10 + 1 + 8 + 4
	w := m.width - fixed
	if w < 40 {
		w = 40
	}
	return w
}

func (m Model) renderList(maxRows int) string {
	mw := m.methodColumnWidth()
	header := fmt.Sprintf("  %-*s %-12s %-10s %s", mw, "Method", "Status", "Latency", "Time")
	lines := []string{headerStyle.Render(header)}

	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
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

		line := fmt.Sprintf("%s%-*s %-12s %-10s %s",
			cursor,
			mw,
			truncate(ev.GetMethod(), mw),
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

func (m Model) renderDetail(maxLines int) string {
	if len(m.events) == 0 {
		return borderStyle.Width(m.width - 2).Render("No events yet.")
	}

	ev := m.events[m.cursor]

	var b strings.Builder
	b.WriteString(labelStyle.Render("Method: "))
	b.WriteString(ev.GetMethod())
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("Status: "))
	b.WriteString(domain.StatusCode(ev.GetStatusCode()).String())
	if msg := ev.GetStatusMessage(); msg != "" {
		b.WriteString(fmt.Sprintf(" (%s)", msg))
	}

	if ev.GetDuration() != nil {
		b.WriteString("  ")
		b.WriteString(labelStyle.Render("Latency: "))
		b.WriteString(ev.GetDuration().AsDuration().String())
	}
	b.WriteString("\n")

	jsonWidth := m.width - 6 // border(2) + padding(2) + margin(2)
	if ev.GetRequestPayload() != "" {
		b.WriteString(labelStyle.Render("Request: "))
		b.WriteString(prettyJSON(ev.GetRequestPayload(), jsonWidth, jsonTruncate))
		b.WriteString("\n")
	}

	if ev.GetResponsePayload() != "" {
		b.WriteString(labelStyle.Render("Response: "))
		b.WriteString(prettyJSON(ev.GetResponsePayload(), jsonWidth, jsonTruncate))
	}

	content := b.String()
	lines := strings.Split(content, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines-1]
		lines = append(lines, helpStyle.Render("..."))
	}

	return borderStyle.Width(m.width - 2).Render(strings.Join(lines, "\n"))
}

func (m Model) renderReplayResult() string {
	if m.replayResult == nil {
		return ""
	}

	var b strings.Builder

	b.WriteString(labelStyle.Render("Method: "))
	b.WriteString(m.replayResult.method)
	b.WriteString("\n")

	if m.replayResult.err != nil {
		b.WriteString(errorStyle.Render("Error: "))
		b.WriteString(m.replayResult.err.Error())
		b.WriteString("\n")

		if strings.Contains(m.replayResult.err.Error(), "Unimplemented") {
			b.WriteString("The server may not have reflection enabled.\n")
			b.WriteString("Add to your server:\n")
			b.WriteString("  import \"google.golang.org/grpc/reflection\"\n")
			b.WriteString("  reflection.Register(srv)\n")
		}
	} else {
		r := m.replayResult.result
		if r.StatusCode == 0 {
			b.WriteString(successStyle.Render("Status: OK"))
		} else {
			b.WriteString(errorStyle.Render(fmt.Sprintf("Status: %s", codes.Code(r.StatusCode).String())))
			if r.StatusMessage != "" {
				b.WriteString(fmt.Sprintf(" (%s)", r.StatusMessage))
			}
		}
		b.WriteString("  ")
		b.WriteString(labelStyle.Render("Duration: "))
		b.WriteString(r.Duration.String())
		b.WriteString("\n")

		if m.replayResult.requestJSON != "" {
			b.WriteString(labelStyle.Render("Request: "))
			b.WriteString(prettyJSON(m.replayResult.requestJSON, m.width-6, jsonWrap))
			b.WriteString("\n")
		}

		if r.ResponseJSON != "" {
			b.WriteString(labelStyle.Render("Response: "))
			b.WriteString(prettyJSON(r.ResponseJSON, m.width-6, jsonWrap))
		}
	}

	allLines := strings.Split(b.String(), "\n")
	m.replayResult.totalLines = len(allLines)

	// Visible area: border(2) + visible + help(1) = m.height
	visibleMax := m.height - 2 - 1
	if visibleMax < 3 {
		visibleMax = 3
	}

	// Clamp scroll offset.
	maxScroll := len(allLines) - visibleMax
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.replayResult.scroll > maxScroll {
		m.replayResult.scroll = maxScroll
	}

	// Slice visible window.
	start := m.replayResult.scroll
	end := start + visibleMax
	if end > len(allLines) {
		end = len(allLines)
	}
	visible := allLines[start:end]

	// Pad to push help text to the bottom.
	pad := visibleMax - len(visible)
	for range pad {
		visible = append(visible, "")
	}
	visible = append(visible, helpStyle.Render("q: back  j/k/↑/↓: scroll  r: resend"))

	return borderStyle.Width(m.width - 2).Render(strings.Join(visible, "\n"))
}

func (m Model) renderHelp() string {
	parts := []string{"q: quit", "j/k/↑/↓: navigate"}
	if m.appTarget != "" && len(m.events) > 0 {
		parts = append(parts, "r: replay", "e: edit & replay")
	}
	return helpStyle.Render("  " + strings.Join(parts, "  "))
}

func (m Model) doReplay(ev *scopev1.CallEvent, payloadJSON string) tea.Cmd {
	appTarget := m.appTarget
	method := ev.GetMethod()
	md := metadataFromEvent(ev)

	return func() tea.Msg {
		client, err := replay.NewClient(appTarget)
		if err != nil {
			return ReplayResultMsg{Method: method, RequestJSON: payloadJSON, Err: err}
		}
		defer client.Close()

		result, err := client.Send(context.Background(), replay.Request{
			Method:      method,
			PayloadJSON: payloadJSON,
			Metadata:    md,
		})
		return ReplayResultMsg{Result: result, Method: method, RequestJSON: payloadJSON, Err: err}
	}
}

func (m Model) openEditor(ev *scopev1.CallEvent) tea.Cmd {
	payload := ev.GetRequestPayload()
	if payload == "" {
		payload = "{}"
	}

	tmpFile, err := os.CreateTemp("", "grpc-scope-*.json")
	if err != nil {
		return func() tea.Msg {
			return EditorFinishedMsg{Event: ev, Err: fmt.Errorf("create temp file: %w", err)}
		}
	}

	if _, err := tmpFile.WriteString(payload); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return func() tea.Msg {
			return EditorFinishedMsg{Event: ev, Err: fmt.Errorf("write temp file: %w", err)}
		}
	}
	tmpFile.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	path := tmpFile.Name()
	c := exec.Command(editor, path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return EditorFinishedMsg{Event: ev, Err: fmt.Errorf("editor: %w", err)}
		}
		edited, err := os.ReadFile(path)
		if err != nil {
			return EditorFinishedMsg{Event: ev, Err: fmt.Errorf("read edited file: %w", err)}
		}
		return EditorFinishedMsg{Payload: string(edited), Event: ev}
	})
}

func metadataFromEvent(ev *scopev1.CallEvent) map[string][]string {
	rm := ev.GetRequestMetadata()
	if len(rm) == 0 {
		return nil
	}
	md := make(map[string][]string, len(rm))
	for k, v := range rm {
		md[k] = v.GetValues()
	}
	return md
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

const maxJSONLines = 6

func prettyJSON(s string, maxWidth int, mode jsonDisplayMode) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	lines := strings.Split(buf.String(), "\n")
	if maxWidth > 0 {
		switch mode {
		case jsonTruncate:
			for i, line := range lines {
				if len(line) > maxWidth {
					lines[i] = line[:maxWidth-3] + "..."
				}
			}
			if len(lines) > maxJSONLines {
				lines = append(lines[:maxJSONLines-1], "  ...")
			}
		case jsonWrap:
			var wrapped []string
			for _, line := range lines {
				for len(line) > maxWidth {
					wrapped = append(wrapped, line[:maxWidth])
					line = line[maxWidth:]
				}
				wrapped = append(wrapped, line)
			}
			lines = wrapped
		}
	}
	return strings.Join(lines, "\n")
}

type jsonDisplayMode int

const (
	jsonTruncate jsonDisplayMode = iota
	jsonWrap
)

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
