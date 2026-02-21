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

func TestBuildArgs_NoOptions(t *testing.T) {
	args := buildArgs(executor.Options{})
	if len(args) < 3 {
		t.Fatalf("expected at least 3 args, got %d: %v", len(args), args)
	}
	if args[0] != "npx" || args[1] != "-y" {
		t.Errorf("expected args to start with 'npx -y', got: %v", args)
	}
	if !containsFlag(args, "--acp") {
		t.Errorf("expected --acp flag, got: %v", args)
	}
	if containsFlag(args, "--allow-all-tools") {
		t.Errorf("expected no --allow-all-tools when CopilotAllowAllTools=false, got: %v", args)
	}
}

func TestBuildArgs_AllowAllTools(t *testing.T) {
	args := buildArgs(executor.Options{CopilotAllowAllTools: true})
	if !containsFlag(args, "--allow-all-tools") {
		t.Errorf("expected --allow-all-tools flag, got: %v", args)
	}
}

func TestBuildArgs_WithModel(t *testing.T) {
	args := buildArgs(executor.Options{Model: "gpt-4o"})
	if !containsFlag(args, "--model") {
		t.Errorf("expected --model flag, got: %v", args)
	}
	if !containsValue(args, "gpt-4o") {
		t.Errorf("expected model name in args, got: %v", args)
	}
}

func TestBuildArgs_NpmPackage(t *testing.T) {
	args := buildArgs(executor.Options{})
	found := false
	for _, a := range args {
		if a == npmPackage {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected npm package %q in args, got: %v", npmPackage, args)
	}
}

func TestBuildArgs_ExtraArgs(t *testing.T) {
	args := buildArgs(executor.Options{ExtraArgs: []string{"--debug"}})
	if !containsFlag(args, "--debug") {
		t.Errorf("expected --debug in extra args, got: %v", args)
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient(nil)
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

// TestClient_NilInner_Interrupt verifies Interrupt returns nil when not started.
func TestClient_NilInner_Interrupt(t *testing.T) {
	c := NewClient(nil)
	if err := c.Interrupt(); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

// TestClient_NilInner_SendMessage verifies SendMessage returns ErrExecutorClosed when not started.
func TestClient_NilInner_SendMessage(t *testing.T) {
	c := NewClient(nil)
	err := c.SendMessage(context.Background(), "hello")
	if err != executor.ErrExecutorClosed {
		t.Errorf("expected ErrExecutorClosed, got: %v", err)
	}
}

// TestClient_NilInner_RespondControl verifies RespondControl returns ErrExecutorClosed when not started.
func TestClient_NilInner_RespondControl(t *testing.T) {
	c := NewClient(nil)
	err := c.RespondControl(context.Background(), executor.ControlResponse{})
	if err != executor.ErrExecutorClosed {
		t.Errorf("expected ErrExecutorClosed, got: %v", err)
	}
}

// TestClient_NilInner_Wait verifies Wait returns nil when not started.
func TestClient_NilInner_Wait(t *testing.T) {
	c := NewClient(nil)
	if err := c.Wait(); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

// TestClient_NilInner_Logs verifies Logs returns a closed channel when not started.
func TestClient_NilInner_Logs(t *testing.T) {
	c := NewClient(nil)
	ch := c.Logs()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel from Logs()")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Logs() channel was not closed")
	}
}

// TestClient_NilInner_Done verifies Done returns a closed channel when not started.
func TestClient_NilInner_Done(t *testing.T) {
	c := NewClient(nil)
	select {
	case <-c.Done():
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done() channel was not closed")
	}
}

// TestClient_NilInner_Close verifies Close returns nil when not started.
func TestClient_NilInner_Close(t *testing.T) {
	c := NewClient(nil)
	if err := c.Close(); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

// TestClient_Start_Integration exercises the full Start path using a fake process.
func TestClient_Start_Integration(t *testing.T) {
	script := `printf '{"SessionStart":"sess-copilot"}\n' && printf '{"Message":{"Text":{"text":"hi from copilot"}}}\n'`
	c := NewClient(fakeCmd(script))

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

// TestClient_Start_WithAllowAllTools exercises the CopilotAllowAllTools/auto-approve path.
func TestClient_Start_WithAllowAllTools(t *testing.T) {
	script := `printf '{"SessionStart":"sess-1"}\n'`
	c := NewClient(fakeCmd(script))

	opts := executor.Options{WorkingDir: t.TempDir(), CopilotAllowAllTools: true}
	if err := c.Start(context.Background(), "test", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-c.Done()
}

// TestClient_Wait_AfterStart ensures Wait returns after process completes.
func TestClient_Wait_AfterStart(t *testing.T) {
	script := `printf '{"SessionStart":"sess-2"}\n'`
	c := NewClient(fakeCmd(script))

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

// TestClient_Close_AfterStart ensures Close can be called after Start.
func TestClient_Close_AfterStart(t *testing.T) {
	script := `sleep 60`
	c := NewClient(fakeCmd(script))

	opts := executor.Options{WorkingDir: t.TempDir()}
	if err := c.Start(context.Background(), "test", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = c.Close()
}

// Compile-time interface check.
var _ executor.Executor = (*Client)(nil)

func containsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func containsValue(args []string, value string) bool {
	for _, a := range args {
		if a == value {
			return true
		}
	}
	return false
}
