package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
)

func TestCodexClient(t *testing.T) {
	client := NewClient()
	client.commandRun = mockCommand

	ctx := context.Background()
	opts := executor.Options{WorkingDir: "."}
	err := client.Start(ctx, "hello", opts)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	client.Close()
}

func TestCodexClient_More(t *testing.T) {
	client := NewClient()
	client.commandRun = mockCommand

	t.Run("Interrupt", func(t *testing.T) {
		client.Interrupt()
	})

	t.Run("SendMessage", func(t *testing.T) {
		client.SendMessage(context.Background(), "test")
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

	t.Run("Helpers", func(t *testing.T) {
		client.nextID()
		ptrToRequestID(nextID())
		mustJSON(map[string]string{"a": "b"})
	})
}

func TestCodexClient_Full(t *testing.T) {
	client := NewClient()
	client.commandRun = mockCommandFull

	ctx := context.Background()
	opts := executor.Options{WorkingDir: "."}
	err := client.Start(ctx, "hello", opts)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Wait for a bit to let the background goroutine run
	time.Sleep(500 * time.Millisecond)
	client.Close()
}

func TestCodexClient_AutoApprovePolicy(t *testing.T) {
	client := NewClient()
	client.commandRun = mockCommand

	if err := client.Start(context.Background(), "hello", executor.Options{WorkingDir: ".", AskForApproval: ""}); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if client.autoApprove {
		t.Fatalf("empty ask_for_approval should not auto-approve by default")
	}
	_ = client.Close()

	client2 := NewClient()
	client2.commandRun = mockCommand
	if err := client2.Start(context.Background(), "hello", executor.Options{WorkingDir: ".", AskForApproval: "never"}); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !client2.autoApprove {
		t.Fatalf("ask_for_approval=never should auto-approve")
	}
	_ = client2.Close()
}

func TestCodexClient_ReadLoopControlRequest(t *testing.T) {
	client := NewClient()
	buf := &bytes.Buffer{}
	client.stdin = nopWriteCloser{Buffer: buf}
	client.autoApprove = true
	input := `{"jsonrpc":"2.0","id":11,"method":"execCommandApproval","params":{"call_id":"call-1"}}` + "\n" +
		`{"jsonrpc":"2.0","method":"codex/event/task_complete","params":{}}` + "\n"

	client.readLoop(context.Background(), strings.NewReader(input))

	var events []executor.Log
	for evt := range client.Logs() {
		events = append(events, evt)
	}

	foundControl := false
	for _, evt := range events {
		if evt.Type == "control_request" {
			foundControl = true
			break
		}
	}
	if !foundControl {
		t.Fatalf("expected control_request event in read loop")
	}
	if !strings.Contains(buf.String(), "\"decision\":\"approved\"") {
		t.Fatalf("expected auto approval response, got %s", buf.String())
	}
}

func TestCodexHelpers(t *testing.T) {
	idn := int64(8)
	if got := requestIDToString(RequestID{Number: &idn}); got != "8" {
		t.Fatalf("unexpected numeric request id: %s", got)
	}
	ids := "abc"
	if got := requestIDToString(RequestID{String: &ids}); got != "abc" {
		t.Fatalf("unexpected string request id: %s", got)
	}
	if !isControlMethod("applyPatchApproval", nil) {
		t.Fatalf("expected approval method detected")
	}
	if !isControlMethod("other", json.RawMessage(`{"call_id":"x"}`)) {
		t.Fatalf("expected call_id payload detected")
	}
	if isControlMethod("other", json.RawMessage(`{"k":"v"}`)) {
		t.Fatalf("unexpected control method detection")
	}

	client := NewClient()
	client.stdin = nopWriteCloser{Buffer: &bytes.Buffer{}}
	if err := client.sendJSON(map[string]string{"k": "v"}); err != nil {
		t.Fatalf("sendJSON should work with writer: %v", err)
	}
	client.stdin = nil
	if err := client.sendJSON(map[string]string{"k": "v"}); err == nil {
		t.Fatalf("expected sendJSON error when stdin is nil")
	}
	if _, err := client.sendRequest(JSONRPCMessage{}); err == nil {
		t.Fatalf("expected sendRequest error for nil id")
	}
}

func TestCodexClient_RPCMethods(t *testing.T) {
	t.Run("initialize", func(t *testing.T) {
		client := NewClient()
		client.stdin = nopWriteCloser{Buffer: &bytes.Buffer{}}
		respondPendingOnce(client, 2, JSONRPCMessage{JSONRPC: "2.0", ID: &RequestID{Number: int64Ptr(2)}, Result: mustJSON(map[string]any{})})
		if err := client.initialize(); err != nil {
			t.Fatalf("initialize failed: %v", err)
		}
	})

	t.Run("newConversation", func(t *testing.T) {
		client := NewClient()
		client.stdin = nopWriteCloser{Buffer: &bytes.Buffer{}}
		respondPendingOnce(client, 2, JSONRPCMessage{JSONRPC: "2.0", ID: &RequestID{Number: int64Ptr(2)}, Result: mustJSON(map[string]any{"conversationId": "conv-new"})})
		id, err := client.newConversation(executor.Options{WorkingDir: ".", AskForApproval: "never"})
		if err != nil {
			t.Fatalf("newConversation failed: %v", err)
		}
		if id != "conv-new" {
			t.Fatalf("unexpected conversation id: %s", id)
		}
	})

	t.Run("resumeConversation", func(t *testing.T) {
		client := NewClient()
		client.stdin = nopWriteCloser{Buffer: &bytes.Buffer{}}
		respondPendingOnce(client, 2, JSONRPCMessage{JSONRPC: "2.0", ID: &RequestID{Number: int64Ptr(2)}, Result: mustJSON(map[string]any{"conversationId": "conv-resume"})})
		id, err := client.resumeConversation(executor.Options{WorkingDir: ".", ResumeSessionID: "conv-old", ResumePath: "/tmp/rollout.jsonl"})
		if err != nil {
			t.Fatalf("resumeConversation failed: %v", err)
		}
		if id != "conv-resume" {
			t.Fatalf("unexpected resumed id: %s", id)
		}
	})

	t.Run("startOrResumeConversation", func(t *testing.T) {
		client := NewClient()
		client.stdin = nopWriteCloser{Buffer: &bytes.Buffer{}}
		respondPendingOnce(client, 2, JSONRPCMessage{JSONRPC: "2.0", ID: &RequestID{Number: int64Ptr(2)}, Result: mustJSON(map[string]any{"conversationId": "conv-a"})})
		id, err := client.startOrResumeConversation(executor.Options{WorkingDir: "."})
		if err != nil || id != "conv-a" {
			t.Fatalf("new path failed: id=%s err=%v", id, err)
		}

		client2 := NewClient()
		client2.stdin = nopWriteCloser{Buffer: &bytes.Buffer{}}
		respondPendingOnce(client2, 2, JSONRPCMessage{JSONRPC: "2.0", ID: &RequestID{Number: int64Ptr(2)}, Result: mustJSON(map[string]any{"conversationId": "conv-b"})})
		id, err = client2.startOrResumeConversation(executor.Options{WorkingDir: ".", ResumeSessionID: "conv-old"})
		if err != nil || id != "conv-b" {
			t.Fatalf("resume path failed: id=%s err=%v", id, err)
		}
	})

	t.Run("addListener", func(t *testing.T) {
		client := NewClient()
		client.stdin = nopWriteCloser{Buffer: &bytes.Buffer{}}
		respondPendingOnce(client, 2, JSONRPCMessage{JSONRPC: "2.0", ID: &RequestID{Number: int64Ptr(2)}, Result: mustJSON(map[string]any{})})
		if err := client.addListener("conv-1"); err != nil {
			t.Fatalf("addListener failed: %v", err)
		}
	})
}

func TestCtxDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := ctxDone(ctx)
	cancel()
	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected done signal")
	}
}

func respondPendingOnce(c *Client, id int64, resp JSONRPCMessage) {
	go func() {
		for i := 0; i < 200; i++ {
			c.pendingMu.Lock()
			ch, ok := c.pending[id]
			c.pendingMu.Unlock()
			if ok {
				ch <- resp
				return
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()
}

func int64Ptr(v int64) *int64 { return &v }

func TestCodexClient_RespondControl(t *testing.T) {
	client := NewClient()
	buf := &bytes.Buffer{}
	client.stdin = nopWriteCloser{Buffer: buf}
	id := int64(7)
	client.control["7"] = RequestID{Number: &id}

	if err := client.RespondControl(context.Background(), executor.ControlResponse{
		RequestID: "7",
		Decision:  executor.ControlDecisionApprove,
	}); err != nil {
		t.Fatalf("respond control failed: %v", err)
	}

	if !strings.Contains(buf.String(), "\"decision\":\"approved\"") {
		t.Fatalf("unexpected response payload: %s", buf.String())
	}
}

func mockCommand(name string, arg ...string) *exec.Cmd {
	args := []string{"-test.run=TestHelperProcess", "--", name}
	args = append(args, arg...)
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return cmd
}

func mockCommandFull(name string, arg ...string) *exec.Cmd {
	args := []string{"-test.run=TestHelperProcessFull", "--", name}
	args = append(args, arg...)
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS_FULL=1")
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)
}

func TestHelperProcessFull(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS_FULL") != "1" {
		return
	}
	defer os.Exit(0)

	// Simulate JSON-RPC responses
	// 1. Initialize response
	fmt.Println(`{"jsonrpc":"2.0","id":2,"result":{}}`)
	// 2. NewConversation response
	fmt.Println(`{"jsonrpc":"2.0","id":3,"result":{"conversationId":"conv-123"}}`)
	// 3. AddListener response
	fmt.Println(`{"jsonrpc":"2.0","id":4,"result":{}}`)
	// 4. SendUserMessage response
	fmt.Println(`{"jsonrpc":"2.0","id":5,"result":{}}`)
	// 5. Task complete event
	fmt.Println(`{"jsonrpc":"2.0","method":"codex/event/task_complete","params":{}}`)
}

type nopWriteCloser struct {
	*bytes.Buffer
}

func (n nopWriteCloser) Close() error { return nil }
