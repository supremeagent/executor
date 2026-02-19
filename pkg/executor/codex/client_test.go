package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/anthropics/vibe-kanban/go-api/pkg/executor"
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
