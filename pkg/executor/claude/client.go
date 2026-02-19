package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/mylxsw/asteria/log"
	"github.com/supremeagent/executor/pkg/executor"
)

// Client implements the Executor interface for Claude Code
type Client struct {
	cmd        *exec.Cmd
	logsChan   chan executor.Log
	doneChan   chan struct{}
	closeOnce  sync.Once
	closed     bool
	mu         sync.Mutex
	commandRun func(name string, arg ...string) *exec.Cmd
}

// NewClient creates a new Claude Code client
func NewClient() *Client {
	return &Client{
		logsChan:   make(chan executor.Log, 100),
		doneChan:   make(chan struct{}),
		commandRun: exec.Command,
	}
}

// Start starts the Claude Code executor with the given prompt
func (c *Client) Start(ctx context.Context, prompt string, opts executor.Options) error {
	// Build command - use -p for non-interactive mode with JSON output
	// -p takes the prompt directly as an argument
	args := []string{"-y", "@anthropic-ai/claude-code@latest", "--print", prompt, "--output-format", "stream-json", "--verbose"}

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Plan {
		args = append(args, "--plan")
	}

	// Always skip permissions for API usage
	args = append(args, "--dangerously-skip-permissions")

	// Create command
	cmd := c.commandRun("npx", args...)
	cmd.Dir = opts.WorkingDir
	// Unset CLAUDECODE env to allow running inside Claude Code session.
	cmd.Env = executor.BuildCommandEnv(opts.Env, map[string]string{"CLAUDECODE": ""})

	// Log the command being executed (mask the prompt in logs for brevity)
	c.sendLog(executor.Log{Type: "command", Content: fmt.Sprintf("npx %s, env: %s", strings.Join(args, " "), strings.Join(cmd.Env, " "))})

	// Use PTY to get unbuffered output from Node.js
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start command with pty: %w", err)
	}

	c.cmd = cmd

	// Parse and send output line by line in background
	go func() {
		defer c.Close()
		defer ptmx.Close()

		scanner := bufio.NewScanner(ptmx)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large JSON lines
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
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
						c.sendLog(executor.Log{Type: "error", Content: result.Result})
					} else {
						c.sendLog(executor.Log{Type: "result", Content: result.Result})
					}
					c.sendLog(executor.Log{Type: "done", Content: line})
					return
				}
			}

			// Send as stdout for any other output
			c.sendLog(executor.Log{Type: "stdout", Content: line})
		}

		if err := cmd.Wait(); err != nil {
			c.sendLog(executor.Log{Type: "error", Content: err.Error()})
		}

		c.sendLog(executor.Log{Type: "done", Content: "Claude execution finished"})
	}()

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
		c.mu.Lock()
		c.closed = true
		close(c.logsChan)
		close(c.doneChan)
		c.mu.Unlock()

		if c.cmd != nil && c.cmd.Process != nil {
			c.cmd.Process.Kill()
		}
	})
	return nil
}

func (c *Client) sendLog(entry executor.Log) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		log.Debugf("claude.sendLog: channel closed, skipping type=%s", entry.Type)
		return
	}
	log.Debugf("claude.sendLog: sending type=%s", entry.Type)
	c.logsChan <- entry
	log.Debugf("claude.sendLog: sent type=%s", entry.Type)
}

// Factory creates Claude Code executor instances
type Factory struct{}

func NewFactory() *Factory {
	return &Factory{}
}

func (f *Factory) Create() (executor.Executor, error) {
	return NewClient(), nil
}
