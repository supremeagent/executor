package droid

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
)

// fakeCmd returns a factory that runs a shell script instead of the real binary.
func fakeCmd(script string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", script)
	}
}

func TestBuildArgs_DefaultYolo(t *testing.T) {
	opts := executor.Options{Yolo: true}
	args := buildArgs(opts)
	if !containsFlag(args, "--skip-permissions-unsafe") {
		t.Errorf("expected --skip-permissions-unsafe for Yolo=true, got: %v", args)
	}
}

func TestBuildArgs_DefaultEmpty(t *testing.T) {
	// Empty DroidAutonomy with Yolo=false should still produce --skip-permissions-unsafe
	// because Droid's default is skip-permissions-unsafe.
	opts := executor.Options{}
	args := buildArgs(opts)
	if !containsFlag(args, "--skip-permissions-unsafe") {
		t.Errorf("expected --skip-permissions-unsafe by default, got: %v", args)
	}
}

func TestBuildArgs_AutonomyLow(t *testing.T) {
	opts := executor.Options{DroidAutonomy: string(AutonomyLow)}
	args := buildArgs(opts)
	if !containsFlag(args, "--auto") {
		t.Errorf("expected --auto flag for AutonomyLow, got: %v", args)
	}
	if !containsValue(args, "low") {
		t.Errorf("expected 'low' after --auto, got: %v", args)
	}
}

func TestBuildArgs_AutonomyMedium(t *testing.T) {
	opts := executor.Options{DroidAutonomy: string(AutonomyMedium)}
	args := buildArgs(opts)
	if !containsFlag(args, "--auto") {
		t.Errorf("expected --auto flag for AutonomyMedium, got: %v", args)
	}
	if !containsValue(args, "medium") {
		t.Errorf("expected 'medium' after --auto, got: %v", args)
	}
}

func TestBuildArgs_AutonomyHigh(t *testing.T) {
	opts := executor.Options{DroidAutonomy: string(AutonomyHigh)}
	args := buildArgs(opts)
	if !containsFlag(args, "--auto") {
		t.Errorf("expected --auto flag for AutonomyHigh, got: %v", args)
	}
	if !containsValue(args, "high") {
		t.Errorf("expected 'high' after --auto, got: %v", args)
	}
}

func TestBuildArgs_AutonomyNormal(t *testing.T) {
	opts := executor.Options{DroidAutonomy: string(AutonomyNormal)}
	args := buildArgs(opts)
	if containsFlag(args, "--auto") || containsFlag(args, "--skip-permissions-unsafe") {
		t.Errorf("expected no autonomy flags for AutonomyNormal, got: %v", args)
	}
}

func TestBuildArgs_WithModel(t *testing.T) {
	opts := executor.Options{Model: "claude-sonnet-4-6", DroidAutonomy: string(AutonomyNormal)}
	args := buildArgs(opts)
	if !containsFlag(args, "--model") {
		t.Errorf("expected --model flag, got: %v", args)
	}
	if !containsValue(args, "claude-sonnet-4-6") {
		t.Errorf("expected model name in args, got: %v", args)
	}
}

func TestBuildArgs_WithReasoningEffort(t *testing.T) {
	opts := executor.Options{
		DroidAutonomy:       string(AutonomyNormal),
		DroidReasoningEffort: string(ReasoningEffortHigh),
	}
	args := buildArgs(opts)
	if !containsFlag(args, "--reasoning-effort") {
		t.Errorf("expected --reasoning-effort flag, got: %v", args)
	}
	if !containsValue(args, "high") {
		t.Errorf("expected 'high' after --reasoning-effort, got: %v", args)
	}
}

func TestBuildArgs_WithResumeSessionID(t *testing.T) {
	opts := executor.Options{
		DroidAutonomy:   string(AutonomyNormal),
		ResumeSessionID: "sess-abc",
	}
	args := buildArgs(opts)
	if !containsFlag(args, "--session-id") {
		t.Errorf("expected --session-id flag, got: %v", args)
	}
	if !containsValue(args, "sess-abc") {
		t.Errorf("expected session id in args, got: %v", args)
	}
}

func TestBuildArgs_ExtraArgs(t *testing.T) {
	opts := executor.Options{
		DroidAutonomy: string(AutonomyNormal),
		ExtraArgs:     []string{"--verbose"},
	}
	args := buildArgs(opts)
	if !containsFlag(args, "--verbose") {
		t.Errorf("expected --verbose in extra args, got: %v", args)
	}
}

func TestBuildArgs_StartsWithDroidExec(t *testing.T) {
	opts := executor.Options{}
	args := buildArgs(opts)
	if len(args) < 2 || args[0] != "droid" || args[1] != "exec" {
		t.Errorf("expected args to start with 'droid exec', got: %v", args)
	}
	if !containsFlag(args, "--output-format") {
		t.Errorf("expected --output-format flag, got: %v", args)
	}
}

func TestClient_StartReceivesSystemEvent(t *testing.T) {
	script := `printf '{"type":"system","session_id":"s1","model":"claude","tools":[]}\n'`
	c := NewClient(fakeCmd(script))

	opts := executor.Options{WorkingDir: t.TempDir(), DroidAutonomy: string(AutonomyNormal)}
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
		if l.Type == "droid_system" {
			found = true
		}
	}
	if !found {
		types := logTypes(logs)
		t.Errorf("expected 'droid_system' log, got: %v", types)
	}
}

func TestClient_StartReceivesMessageEvent(t *testing.T) {
	script := `printf '{"type":"message","role":"assistant","id":"m1","text":"Hello","timestamp":1,"session_id":"s1"}\n'`
	c := NewClient(fakeCmd(script))

	opts := executor.Options{WorkingDir: t.TempDir(), DroidAutonomy: string(AutonomyNormal)}
	if err := c.Start(context.Background(), "test", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-c.Done()
	// Basic smoke test – if we get here without panic, the loop handled the event correctly.
}

func TestClient_StartReceivesCompletionEvent(t *testing.T) {
	script := strings.Join([]string{
		`printf '{"type":"completion","finalText":"all done","session_id":"s1"}\n'`,
	}, " && ")
	c := NewClient(fakeCmd(script))

	opts := executor.Options{WorkingDir: t.TempDir(), DroidAutonomy: string(AutonomyNormal)}
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
		if l.Type == "droid_completion" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'droid_completion' log, got: %v", logTypes(logs))
	}
}

func TestClient_StartReceivesErrorEvent(t *testing.T) {
	script := `printf '{"type":"error","message":"oops","source":"tool","timestamp":1}\n'`
	c := NewClient(fakeCmd(script))

	opts := executor.Options{WorkingDir: t.TempDir(), DroidAutonomy: string(AutonomyNormal)}
	if err := c.Start(context.Background(), "test", opts); err != nil {
		t.Fatalf("Start: %v", err)
	}

	<-c.Done()
}

func TestClient_SendMessage_ReturnsError(t *testing.T) {
	c := NewClient(nil)
	err := c.SendMessage(context.Background(), "hello")
	if err == nil {
		t.Error("expected error: Droid does not support SendMessage")
	}
}

func TestClient_RespondControl_ReturnsError(t *testing.T) {
	c := NewClient(nil)
	err := c.RespondControl(context.Background(), executor.ControlResponse{})
	if err == nil {
		t.Error("expected error: Droid does not support RespondControl")
	}
}

func TestClient_CloseTwice(t *testing.T) {
	c := NewClient(nil)
	_ = c.Close()
	_ = c.Close() // must not panic
}

func TestClient_DoneChannelClosed(t *testing.T) {
	c := NewClient(nil)
	_ = c.Close()
	select {
	case <-c.Done():
	case <-time.After(100 * time.Millisecond):
		t.Error("Done() channel not closed after Close()")
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

// Compile-time interface check.
var _ executor.Executor = (*Client)(nil)

// helpers

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

func logTypes(logs []executor.Log) []string {
	types := make([]string, 0, len(logs))
	for _, l := range logs {
		types = append(types, l.Type)
	}
	return types
}
