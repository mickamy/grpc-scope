package tui_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mickamy/grpc-scope/replay"
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

func setupModelWithEvent(appTarget string) tui.Model {
	m := tui.NewModel("localhost:9090", appTarget)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(tui.Model)

	ev := newTestEvent("evt-1", "/test.v1.Test/Get", 0)
	updated, _ = m.Update(tui.EventMsg{Event: ev})
	return updated.(tui.Model)
}

func TestModel_Update_EventMsg(t *testing.T) {
	t.Parallel()

	m := tui.NewModel("localhost:9090", "")

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

	m := tui.NewModel("localhost:9090", "")

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

	m := tui.NewModel("localhost:9090", "")

	// Move up with no events should not panic
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(tui.Model)

	// Move down with no events should not panic
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	_ = updated.(tui.Model)
}

func TestModel_Update_ErrMsg(t *testing.T) {
	t.Parallel()

	m := tui.NewModel("localhost:9090", "")
	updated, _ := m.Update(tui.ErrMsg{Err: fmt.Errorf("connection refused")})
	model := updated.(tui.Model)

	view := model.View()
	if !strings.Contains(view, "Is the interceptor running") {
		t.Errorf("expected friendly error in view, got:\n%s", view)
	}
}

func TestModel_View_NoEvents(t *testing.T) {
	t.Parallel()

	m := tui.NewModel("localhost:9090", "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(tui.Model)

	view := model.View()
	if !strings.Contains(view, "No events yet") {
		t.Errorf("expected 'No events yet' in view, got:\n%s", view)
	}
}

func TestModel_View_HelpBar(t *testing.T) {
	t.Parallel()

	t.Run("without appTarget", func(t *testing.T) {
		t.Parallel()

		m := setupModelWithEvent("")
		view := m.View()

		if strings.Contains(view, "r: replay") {
			t.Error("replay keys should not appear without appTarget")
		}
		if !strings.Contains(view, "q: quit") {
			t.Error("expected help bar with quit key")
		}
	})

	t.Run("with appTarget", func(t *testing.T) {
		t.Parallel()

		m := setupModelWithEvent("localhost:8080")
		view := m.View()

		if !strings.Contains(view, "r: replay") {
			t.Error("expected replay key in help bar")
		}
		if !strings.Contains(view, "e: edit & replay") {
			t.Error("expected edit & replay key in help bar")
		}
	})
}

func TestModel_Update_ReplayResultMsg(t *testing.T) {
	t.Parallel()

	m := setupModelWithEvent("localhost:8080")

	// Simulate receiving a replay result
	updated, _ := m.Update(tui.ReplayResultMsg{
		Result: &replay.Result{
			ResponseJSON: `{"message":"Hello!"}`,
			StatusCode:   0,
			Duration:     50 * time.Millisecond,
		},
		Method: "/test.v1.Test/Get",
	})
	model := updated.(tui.Model)

	view := model.View()
	if !strings.Contains(view, "Press esc to go back") {
		t.Error("expected replay result view")
	}
	if !strings.Contains(view, "OK") {
		t.Errorf("expected OK status in replay result, got:\n%s", view)
	}
	if !strings.Contains(view, `"message": "Hello!"`) {
		t.Errorf("expected response JSON in replay result, got:\n%s", view)
	}
	if !strings.Contains(view, "esc") {
		t.Error("expected esc hint in replay result view")
	}
}

func TestModel_Update_ReplayResultMsg_Error(t *testing.T) {
	t.Parallel()

	m := setupModelWithEvent("localhost:8080")

	updated, _ := m.Update(tui.ReplayResultMsg{
		Method: "/test.v1.Test/Get",
		Err:    fmt.Errorf("replay: reflection error: Unimplemented"),
	})
	model := updated.(tui.Model)

	view := model.View()
	if !strings.Contains(view, "Unimplemented") {
		t.Error("expected error message in view")
	}
	if !strings.Contains(view, "reflection.Register") {
		t.Error("expected reflection guidance in error view")
	}
}

func TestModel_Update_ReplayResultMsg_NonZeroStatus(t *testing.T) {
	t.Parallel()

	m := setupModelWithEvent("localhost:8080")

	updated, _ := m.Update(tui.ReplayResultMsg{
		Result: &replay.Result{
			StatusCode:    5, // NOT_FOUND
			StatusMessage: "resource not found",
			Duration:      10 * time.Millisecond,
		},
		Method: "/test.v1.Test/Get",
	})
	model := updated.(tui.Model)

	view := model.View()
	if !strings.Contains(view, "NotFound") {
		t.Errorf("expected NotFound status in view, got:\n%s", view)
	}
	if !strings.Contains(view, "resource not found") {
		t.Errorf("expected status message in view, got:\n%s", view)
	}
}

func TestModel_Update_EscFromReplayView(t *testing.T) {
	t.Parallel()

	m := setupModelWithEvent("localhost:8080")

	// Enter replay view
	updated, _ := m.Update(tui.ReplayResultMsg{
		Result: &replay.Result{StatusCode: 0, Duration: time.Millisecond},
		Method: "/test.v1.Test/Get",
	})
	m = updated.(tui.Model)

	// Press esc to go back
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(tui.Model)

	view := m.View()
	if strings.Contains(view, "Press esc to go back") {
		t.Error("expected to return to list view after esc")
	}
	if !strings.Contains(view, "gRPC Traffic") {
		t.Errorf("expected list view after esc, got:\n%s", view)
	}
}

func TestModel_Update_ReplayKeyIgnored_NoAppTarget(t *testing.T) {
	t.Parallel()

	m := setupModelWithEvent("")

	// Press 'r' should be ignored (no appTarget)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = updated.(tui.Model)

	if cmd != nil {
		t.Error("expected no command when replay key pressed without appTarget")
	}
}

func TestModel_Update_ReplayKeyIgnored_NoEvents(t *testing.T) {
	t.Parallel()

	m := tui.NewModel("localhost:9090", "localhost:8080")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(tui.Model)

	// Press 'r' should be ignored (no events)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = updated.(tui.Model)

	if cmd != nil {
		t.Error("expected no command when replay key pressed with no events")
	}
}

func TestModel_Update_CursorIgnoredInReplayView(t *testing.T) {
	t.Parallel()

	m := setupModelWithEvent("localhost:8080")

	// Add a second event
	ev2 := newTestEvent("evt-2", "/test.v1.Test/List", 0)
	updated, _ := m.Update(tui.EventMsg{Event: ev2})
	m = updated.(tui.Model)

	// Enter replay view
	updated, _ = m.Update(tui.ReplayResultMsg{
		Result: &replay.Result{StatusCode: 0, Duration: time.Millisecond},
		Method: "/test.v1.Test/Get",
	})
	m = updated.(tui.Model)

	// Try to navigate — should be ignored in replay view
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(tui.Model)

	view := m.View()
	if !strings.Contains(view, "Press esc to go back") {
		t.Error("expected to stay in replay view")
	}
}

func TestModel_Update_EditorFinishedMsg_Error(t *testing.T) {
	t.Parallel()

	m := setupModelWithEvent("localhost:8080")

	ev := newTestEvent("evt-1", "/test.v1.Test/Get", 0)
	updated, _ := m.Update(tui.EditorFinishedMsg{
		Event: ev,
		Err:   fmt.Errorf("editor: exit status 1"),
	})
	model := updated.(tui.Model)

	view := model.View()
	if !strings.Contains(view, "editor: exit status 1") {
		t.Errorf("expected editor error in view, got:\n%s", view)
	}
}
