package qwen

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
)

func TestQwenClient(t *testing.T) {
	client := NewClient()
	client.commandRun = mockCommand

	ctx := context.Background()
	opts := executor.Options{WorkingDir: "."}
	err := client.Start(ctx, "hello", opts)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	logsChan := client.Logs()
	done := false
	for log := range logsChan {
		if log.Type == "done" {
			done = true
			break
		}
	}

	if !done {
		t.Error("expected done log")
	}
}

func TestQwenClient_More(t *testing.T) {
	client := NewClient()
	client.commandRun = mockCommand

	t.Run("Interrupt", func(t *testing.T) {
		client.Interrupt()
	})

	t.Run("SendMessage", func(t *testing.T) {
		client.SendMessage(context.Background(), "test")
	})

	t.Run("RespondControl", func(t *testing.T) {
		c := NewClient()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		c.ptyFile = w
		c.controls["req-1"] = ControlRequestType{
			Subtype: "can_use_tool",
			Input:   json.RawMessage(`{"cmd":"ls"}`),
		}
		err = c.RespondControl(context.Background(), executor.ControlResponse{
			RequestID: "req-1",
			Decision:  executor.ControlDecisionApprove,
		})
		if err != nil {
			t.Fatalf("respond control failed: %v", err)
		}
		_ = w.Close()
		data, _ := io.ReadAll(r)
		if !strings.Contains(string(data), "req-1") || !strings.Contains(string(data), "\"behavior\":\"allow\"") {
			t.Fatalf("unexpected control payload: %s", string(data))
		}
	})

	t.Run("Wait", func(t *testing.T) {
		client := NewClient()
		client.commandRun = mockCommand
		client.Start(context.Background(), "hello", executor.Options{})
		go func() {
			time.Sleep(10 * time.Millisecond)
			client.Close()
		}()
		client.Wait()
	})

	t.Run("Logs", func(t *testing.T) {
		client.Logs()
	})

	t.Run("Done", func(t *testing.T) {
		client.Done()
	})

	t.Run("Factory", func(t *testing.T) {
		f := NewFactory()
		_, err := f.Create()
		if err != nil {
			t.Error(err)
		}
	})
}

func TestQwenClient_buildControlPayload(t *testing.T) {
	c := NewClient()

	c.controls["req-deny"] = ControlRequestType{Subtype: "can_use_tool"}
	raw, err := c.buildControlPayload(executor.ControlResponse{
		RequestID: "req-deny",
		Decision:  executor.ControlDecisionDeny,
		Reason:    "unsafe command",
	})
	if err != nil {
		t.Fatalf("build payload failed: %v", err)
	}
	if !strings.Contains(string(raw), "\"behavior\":\"deny\"") {
		t.Fatalf("unexpected deny payload: %s", string(raw))
	}

	c.controls["req-hook"] = ControlRequestType{Subtype: "hook_callback"}
	raw, err = c.buildControlPayload(executor.ControlResponse{
		RequestID: "req-hook",
		Decision:  executor.ControlDecisionApprove,
	})
	if err != nil {
		t.Fatalf("hook payload failed: %v", err)
	}
	if !strings.Contains(string(raw), "\"hookSpecificOutput\"") {
		t.Fatalf("unexpected hook payload: %s", string(raw))
	}

	c.controls["req-unknown"] = ControlRequestType{Subtype: "unknown"}
	if _, err := c.buildControlPayload(executor.ControlResponse{
		RequestID: "req-unknown",
		Decision:  executor.ControlDecisionApprove,
	}); err == nil {
		t.Fatalf("expected error for unknown subtype")
	}

	c.controls["req-bad-input"] = ControlRequestType{
		Subtype: "can_use_tool",
		Input:   json.RawMessage(`{"invalid"`),
	}
	if _, err := c.buildControlPayload(executor.ControlResponse{
		RequestID: "req-bad-input",
		Decision:  executor.ControlDecisionApprove,
	}); err == nil {
		t.Fatalf("expected error for invalid tool input")
	}

	if _, err := c.buildControlPayload(executor.ControlResponse{
		RequestID: "missing",
		Decision:  executor.ControlDecisionApprove,
	}); err == nil {
		t.Fatalf("expected error for missing request id")
	}
}

func TestQwenClient_trackControlRequest(t *testing.T) {
	c := NewClient()
	c.trackControlRequest(map[string]any{
		"type":       "control_request",
		"request_id": "req-10",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input": map[string]any{
				"cmd": "pwd",
			},
		},
	})
	if _, ok := c.controls["req-10"]; !ok {
		t.Fatalf("expected control request tracked")
	}

	c.trackControlRequest(map[string]any{"type": "control_request"})
}

func TestParseJSONFromLine(t *testing.T) {
	if _, ok := parseJSONFromLine("not-json"); ok {
		t.Fatalf("expected parse failure")
	}
	obj, ok := parseJSONFromLine("prefix {\"type\":\"result\"}")
	if !ok || obj["type"] != "result" {
		t.Fatalf("expected parsed result object, got %#v", obj)
	}
}

func mockCommand(name string, arg ...string) *exec.Cmd {
	args := []string{"-test.run=TestHelperProcess", "--", name}
	args = append(args, arg...)
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	// Simulate Claude JSON output
	fmt.Println(`{"type": "stdout", "content": "thinking..."}`)
	fmt.Println(`{"type": "result", "result": "Hello! I am Claude.", "is_error": false}`)
}
