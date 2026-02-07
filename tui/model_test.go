package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	scopev1 "github.com/mickamy/grpc-scope/scope/gen/scope/v1"
	"github.com/mickamy/grpc-scope/tui"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func newTestEvent(id, method string, statusCode int32) *scopev1.CallEvent {
	return &scopev1.CallEvent{
		Id:              id,
		Method:          method,
		StatusCode:      statusCode,
		StartTime:       timestamppb.Now(),
		Duration:        durationpb.New(10_000_000), // 10ms
		RequestPayload:  `{"key":"value"}`,
		ResponsePayload: `{"result":"ok"}`,
	}
}

func TestModel_Update_EventMsg(t *testing.T) {
	t.Parallel()

	m := tui.NewModel("localhost:9090")

	ev := newTestEvent("evt-1", "/test.v1.Test/Get", 1)
	updated, _ := m.Update(tui.EventMsg{Event: ev})

	model := updated.(tui.Model)
	view := model.View()

	if view == "Connecting..." {
		// need to set window size first
		sized, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = sized.(tui.Model)
		view = model.View()
	}

	if !strings.Contains(view, "/test.v1.Test/Get") {
		t.Errorf("expected view to contain method name, got:\n%s", view)
	}
}

func TestModel_Update_CursorNavigation(t *testing.T) {
	t.Parallel()

	m := tui.NewModel("localhost:9090")

	// Set window size
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(tui.Model)

	// Add events
	for i := range 3 {
		ev := newTestEvent(
			string(rune('a'+i)),
			"/test.v1.Test/Method"+string(rune('A'+i)),
			1,
		)
		updated, _ = m.Update(tui.EventMsg{Event: ev})
		m = updated.(tui.Model)
	}

	// Cursor starts at 0, move down
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(tui.Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(tui.Model)

	// Should show detail for third event (cursor=2)
	view := m.View()
	if !strings.Contains(view, "/test.v1.Test/MethodC") {
		t.Errorf("expected detail to show third method, got:\n%s", view)
	}

	// Move up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(tui.Model)

	view = m.View()
	if !strings.Contains(view, "/test.v1.Test/MethodB") {
		t.Errorf("expected detail to show second method, got:\n%s", view)
	}
}

func TestModel_Update_CursorBounds(t *testing.T) {
	t.Parallel()

	m := tui.NewModel("localhost:9090")

	// Move up with no events should not panic
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(tui.Model)

	// Move down with no events should not panic
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	_ = updated.(tui.Model)
}

func TestModel_Update_ErrMsg(t *testing.T) {
	t.Parallel()

	m := tui.NewModel("localhost:9090")
	updated, _ := m.Update(tui.ErrMsg{Err: fmt.Errorf("connection refused")})
	model := updated.(tui.Model)

	view := model.View()
	if !strings.Contains(view, "Is the interceptor running") {
		t.Errorf("expected friendly error in view, got:\n%s", view)
	}
}

func TestModel_View_NoEvents(t *testing.T) {
	t.Parallel()

	m := tui.NewModel("localhost:9090")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(tui.Model)

	view := model.View()
	if !strings.Contains(view, "No events yet") {
		t.Errorf("expected 'No events yet' in view, got:\n%s", view)
	}
}

