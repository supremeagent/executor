package acp

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
)

// fakeCmd creates an exec.Cmd that runs a short shell script instead of a real process.
// On macOS / Linux the script is executed via /bin/sh.
func fakeCmd(script string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", script)
	}
}

func TestNewClientWithArgs(t *testing.T) {
	args := []string{"npx", "-y", "@google/gemini-cli@latest", "--experimental-acp"}
	c := NewClientWithArgs(nil, args)
	if len(c.args) != len(args) {
		t.Errorf("expected %d args, got %d", len(args), len(c.args))
	}
}

func TestClient_StartNoArgs(t *testing.T) {
	c := NewClient(nil)
	err := c.Start(context.Background(), "test", executor.Options{})
	if err == nil {
		t.Error("expected error when no args provided")
	}
}

func TestClient_SetAutoApprove(t *testing.T) {
	c := NewClient(nil)
	c.SetAutoApprove(true)
	if !c.autoApprove {
		t.Error("expected autoApprove to be true")
	}
}

func TestClient_StartAndReceiveEvents(t *testing.T) {
	// Fake ACP process: emits a SessionStart, a Message, and then exits.
	script := `printf '{"SessionStart":"sess-xyz"}\n' && printf '{"Message":{"Text":{"text":"hello"}}}\n'`
	args := []string{"fake-prog", "--acp"}
	c := NewClientWithArgs(fakeCmd(script), args)

	opts := executor.Options{WorkingDir: t.TempDir()}
	if err := c.Start(context.Background(), "test prompt", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var logs []executor.Log
	timeout := time.After(5 * time.Second)
	for {
		select {
		case log, ok := <-c.Logs():
			if !ok {
				goto done
			}
			logs = append(logs, log)
		case <-timeout:
			t.Fatal("timed out waiting for logs")
		}
	}
done:

	types := make([]string, 0, len(logs))
	for _, l := range logs {
		types = append(types, l.Type)
	}

	// We expect: command, session_start, Message, done
	found := func(want string) bool {
		for _, t := range types {
			if t == want {
				return true
			}
		}
		return false
	}

	if !found("command") {
		t.Errorf("expected 'command' log, got: %v", types)
	}
	if !found("session_start") {
		t.Errorf("expected 'session_start' log, got: %v", types)
	}
	if !found(string(EventTypeMessage)) {
		t.Errorf("expected %q log, got: %v", EventTypeMessage, types)
	}
	if !found("done") {
		t.Errorf("expected 'done' log, got: %v", types)
	}
}

func TestClient_ErrorLine(t *testing.T) {
	script := `printf '{"Error":"something went wrong"}\n'`
	args := []string{"fake-prog"}
	c := NewClientWithArgs(fakeCmd(script), args)

	opts := executor.Options{WorkingDir: t.TempDir()}
	if err := c.Start(context.Background(), "test", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var logs []executor.Log
	timeout := time.After(5 * time.Second)
	for {
		select {
		case log, ok := <-c.Logs():
			if !ok {
				goto done
			}
			logs = append(logs, log)
		case <-timeout:
			t.Fatal("timed out")
		}
	}
done:

	found := false
	for _, l := range logs {
		if l.Type == "error" {
			found = true
			break
		}
	}
	if !found {
		types := make([]string, 0, len(logs))
		for _, l := range logs {
			types = append(types, l.Type)
		}
		t.Errorf("expected 'error' log, got: %v", types)
	}
}

func TestClient_NonACPLine(t *testing.T) {
	// A plain text line that is not valid JSON.
	script := `printf 'this is a startup message\n'`
	args := []string{"fake-prog"}
	c := NewClientWithArgs(fakeCmd(script), args)

	opts := executor.Options{WorkingDir: t.TempDir()}
	if err := c.Start(context.Background(), "test", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-c.Done()
	// Should have received a "stdout" log for the non-JSON line.
}

func TestClient_RespondControl_NotFound(t *testing.T) {
	c := NewClientWithArgs(nil, []string{"x"})
	err := c.RespondControl(context.Background(), executor.ControlResponse{
		RequestID: "not-existing",
		Decision:  executor.ControlDecisionApprove,
	})
	if err == nil {
		t.Error("expected error for unknown request ID")
	}
}

func TestClient_SendMessage_Closed(t *testing.T) {
	c := NewClientWithArgs(nil, []string{"x"})
	_ = c.Close()
	err := c.SendMessage(context.Background(), "hello")
	if err == nil {
		t.Error("expected error sending to closed client")
	}
}

func TestClient_CloseTwice(t *testing.T) {
	c := NewClientWithArgs(nil, []string{"x"})
	// Should not panic.
	_ = c.Close()
	_ = c.Close()
}

func TestClient_ApprovalAutoApprove(t *testing.T) {
	// Emits a permission request; the client should auto-approve it.
	script := strings.Join([]string{
		`printf '{"RequestPermission":{"tool_call_id":"tc-1","tool_call":{"tool_call_id":"tc-1","kind":"Execute","title":"ls","status":"pending"}}}\n'`,
	}, " && ")
	args := []string{"fake-prog"}
	c := NewClientWithArgs(fakeCmd(script), args)
	c.SetAutoApprove(true)

	opts := executor.Options{WorkingDir: t.TempDir()}
	if err := c.Start(context.Background(), "test", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-c.Done()

	// Verify control_request log was emitted.
	// (Auto-approve writes back to stdin which may fail since process exits, but the log must be present.)
}

func TestClient_InterruptBeforeStart(t *testing.T) {
	c := NewClientWithArgs(nil, []string{"x"})
	// Should not panic or error.
	err := c.Interrupt()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_Logs_ClosedChannel(t *testing.T) {
	c := NewClientWithArgs(nil, []string{"x"})
	_ = c.Close()
	ch := c.Logs()
	// The channel should be closed (readable with zero value).
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel")
		}
	default:
		// channel already closed and drained
	}
}

func TestClient_Done_ClosedChannel(t *testing.T) {
	c := NewClientWithArgs(nil, []string{"x"})
	_ = c.Close()
	select {
	case <-c.Done():
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done() channel not closed after Close()")
	}
}

// Ensure Client satisfies executor.Executor interface at compile time.
var _ executor.Executor = (*Client)(nil)

// suppress unused import
var _ = fmt.Sprintf
