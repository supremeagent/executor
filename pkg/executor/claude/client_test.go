package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
)

func TestClaudeClient(t *testing.T) {
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

func TestClaudeClient_More(t *testing.T) {
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
