package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/anthropics/vibe-kanban/go-api/pkg/executor"
)

// Client implements the Executor interface for Claude Code
type Client struct {
	cmd       *exec.Cmd
	logsChan  chan executor.Log
	doneChan  chan struct{}
	closeOnce sync.Once
}

// NewClient creates a new Claude Code client
func NewClient() *Client {
	return &Client{
		logsChan: make(chan executor.Log, 100),
		doneChan: make(chan struct{}),
	}
}

// Start starts the Claude Code executor with the given prompt
func (c *Client) Start(ctx context.Context, prompt string, opts executor.Options) error {
	// Build command - use -p for non-interactive mode with JSON output
	args := []string{"-y", "@anthropic-ai/claude-code@latest", "-p", "--output-format", "json"}

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Plan {
		args = append(args, "--plan")
	}

	// Always skip permissions for API usage
	args = append(args, "--dangerously-skip-permissions")

	// Prepare the input message
	userMsg := struct {
		Type  string `json:"type"`
		User  struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"user"`
	}{
		Type: "message",
		User: struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			Role:    "user",
			Content: prompt,
		},
	}

	msgBytes, _ := json.Marshal(userMsg)

	// Create command
	cmd := exec.Command("npx", args...)
	cmd.Dir = opts.WorkingDir
	// Unset CLAUDECODE env to allow running inside Claude Code session
	cmd.Env = append(os.Environ(), "CLAUDECODE=")
	// Pass the message via stdin
	cmd.Stdin = strings.NewReader(string(msgBytes) + "\n")

	// Run command and capture output
	output, err := cmd.CombinedOutput()

	c.cmd = cmd

	// Parse and send output line by line
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try to parse as JSON
		var result struct {
			Type    string `json:"type"`
			Result  string `json:"result"`
			IsError bool   `json:"is_error"`
		}
		if parseErr := json.Unmarshal([]byte(line), &result); parseErr == nil {
			if result.Type == "result" {
				if result.IsError {
					c.logsChan <- executor.Log{Type: "error", Content: result.Result}
				} else {
					c.logsChan <- executor.Log{Type: "result", Content: result.Result}
				}
				// Send done - don't close here, let Close() handle it
				c.logsChan <- executor.Log{Type: "done", Content: line}
				return nil
			}
		}

		// Send as stdout for any other output
		c.logsChan <- executor.Log{Type: "stdout", Content: line}
	}

	if err != nil {
		c.logsChan <- executor.Log{Type: "error", Content: err.Error()}
	}

	c.logsChan <- executor.Log{Type: "done", Content: string(output)}
	return nil
}

// Interrupt interrupts the current execution
func (c *Client) Interrupt() error {
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return nil
}

// SendMessage sends a message to continue the conversation
func (c *Client) SendMessage(ctx context.Context, message string) error {
	return fmt.Errorf("continue not implemented for Claude Code CLI")
}

// Wait waits for the execution to complete
func (c *Client) Wait() error {
	<-c.doneChan
	return nil
}

// Logs returns a channel for streaming logs
func (c *Client) Logs() <-chan executor.Log {
	return c.logsChan
}

// Done returns a channel that signals when execution is complete
func (c *Client) Done() <-chan struct{} {
	return c.doneChan
}

// Close closes the executor and releases resources
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.logsChan)
		close(c.doneChan)
	})
	return nil
}

// Factory creates Claude Code executor instances
type Factory struct{}

func NewFactory() *Factory {
	return &Factory{}
}

func (f *Factory) Create() (executor.Executor, error) {
	return NewClient(), nil
}
