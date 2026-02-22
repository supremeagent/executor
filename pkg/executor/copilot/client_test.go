package copilot

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
)

// fakeCmd returns an exec.Cmd that runs a shell script instead of a real process.
func fakeCmd(script string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", script)
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestFactory_Create(t *testing.T) {
	f := NewFactory()
	exec, err := f.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if exec == nil {
		t.Error("expected non-nil executor")
	}
}

// TestClient_Start_Integration exercises the full Start path using a fake process.
func TestClient_Start_Integration(t *testing.T) {
	script := `echo "hi from copilot"`
	c := NewClient()
	c.commandRun = fakeCmd(script)

	opts := executor.Options{WorkingDir: t.TempDir()}
	if err := c.Start(context.Background(), "do something", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var logTypes []string
	timeout := time.After(5 * time.Second)
	for {
		select {
		case log, ok := <-c.Logs():
			if !ok {
				goto done
			}
			logTypes = append(logTypes, log.Type)
		case <-timeout:
			t.Fatal("timed out waiting for logs")
		}
	}
done:
	found := func(want string) bool {
		for _, lt := range logTypes {
			if lt == want {
				return true
			}
		}
		return false
	}
	if !found("done") {
		t.Errorf("expected 'done' log, got: %v", logTypes)
	}
}

// TestClient_Wait_AfterStart ensures Wait returns after process completes.
func TestClient_Wait_AfterStart(t *testing.T) {
	script := `echo "sess-2"`
	c := NewClient()
	c.commandRun = fakeCmd(script)

	opts := executor.Options{WorkingDir: t.TempDir()}
	if err := c.Start(context.Background(), "test", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Wait() did not return in time")
	}
}

// Compile-time interface check.
var _ executor.Executor = (*Client)(nil)
