package qwen

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/supremeagent/executor/pkg/executor"
)

// Client implements the Executor interface for Qwen Code
type Client struct {
	cmd        *exec.Cmd
	ptyFile    *os.File
	logsChan   chan executor.Log
	doneChan   chan struct{}
	closeOnce  sync.Once
	closed     bool
	mu         sync.Mutex
	controls   map[string]ControlRequestType
	commandRun func(name string, arg ...string) *exec.Cmd
}

// NewClient creates a new Qwen Code client
func NewClient() *Client {
	return &Client{
		logsChan:   make(chan executor.Log, 100),
		doneChan:   make(chan struct{}),
		controls:   make(map[string]ControlRequestType),
		commandRun: exec.Command,
	}
}

// Start starts the Qwen Code executor with the given prompt
func (c *Client) Start(ctx context.Context, prompt string, opts executor.Options) error {
	args := []string{"-y", "--package", "@qwen-code/qwen-code@latest", "qwen", prompt, "--output-format", "stream-json"}

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	if opts.Plan {
		args = append(args, "--plan")
	}
	if opts.Yolo || opts.DangerouslySkipPermissions {
		args = append(args, "--yolo")
	} else {
		args = append(args, "--permission-prompt-tool", "stdio", "--input-format", "stream-json")
	}

	// Create command
	cmd := c.commandRun("npx", args...)
	cmd.Dir = opts.WorkingDir
	// Unset QWEN env if needed (not strictly required, but analogous to Claude)
	cmd.Env = executor.BuildCommandEnv(opts.Env, map[string]string{})

	// Log the command being executed (mask the prompt in logs for brevity)
	c.sendLog(executor.Log{Type: "command", Content: fmt.Sprintf("npx %s", strings.Join(args, " "))})

	// Use PTY to get unbuffered output from Node.js
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start command with pty: %w", err)
	}

	c.cmd = cmd
	c.ptyFile = ptmx

	// Parse and send output line by line in background
	go func() {
		defer c.Close()
		defer ptmx.Close()

		defer c.sendLog(executor.Log{Type: "done", Content: "Qwen execution finished"})

		scanner := bufio.NewScanner(ptmx)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large JSON lines
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			obj, ok := parseJSONFromLine(line)
			if !ok {
				c.sendLog(executor.Log{Type: "stdout", Content: line})
				continue
			}

			typeName, _ := obj["type"].(string)
			switch typeName {
			case "control_request":
				c.trackControlRequest(obj)
				c.sendLog(executor.Log{Type: "control_request", Content: obj})
			case "result":
				result, _ := obj["result"].(string)
				isError, _ := obj["is_error"].(bool)
				if isError {
					c.sendLog(executor.Log{Type: "error", Content: result})
				} else {
					c.sendLog(executor.Log{Type: "result", Content: result})
				}
				c.sendLog(executor.Log{Type: "done", Content: obj})
				return
			default:
				c.sendLog(executor.Log{Type: "stdout", Content: obj})
			}
		}

		if err := cmd.Wait(); err != nil {
			c.sendLog(executor.Log{Type: "error", Content: err.Error()})
		}

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
	msg := NewUserMessage(message)
	return c.writeJSONLine(msg)
}

func (c *Client) RespondControl(ctx context.Context, response executor.ControlResponse) error {
	raw, err := c.buildControlPayload(response)
	if err != nil {
		return err
	}
	msg := ControlResponseMessage(response.RequestID, raw)
	return c.writeJSONLine(msg)
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
		if c.ptyFile != nil {
			_ = c.ptyFile.Close()
		}
		c.controls = nil
	})
	return nil
}

func (c *Client) sendLog(entry executor.Log) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}

	c.logsChan <- entry
}

func (c *Client) writeJSONLine(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ptyFile == nil {
		return executor.ErrExecutorClosed
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := c.ptyFile.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (c *Client) trackControlRequest(obj map[string]any) {
	data, err := json.Marshal(obj)
	if err != nil {
		return
	}
	var req ControlRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}
	if req.RequestID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.controls != nil {
		c.controls[req.RequestID] = req.Request
	}
}

func (c *Client) buildControlPayload(response executor.ControlResponse) (json.RawMessage, error) {
	c.mu.Lock()
	req, ok := c.controls[response.RequestID]
	if ok {
		delete(c.controls, response.RequestID)
	}
	c.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("control request %s not found", response.RequestID)
	}

	switch req.Subtype {
	case "can_use_tool":
		if response.Decision == executor.ControlDecisionApprove {
			var input any = map[string]any{}
			if len(req.Input) > 0 {
				var decoded any
				if err := json.Unmarshal(req.Input, &decoded); err != nil {
					return nil, fmt.Errorf("invalid can_use_tool input: %w", err)
				}
				input = decoded
			}
			return mustRawJSON(map[string]any{
				"behavior":     "allow",
				"updatedInput": input,
			}), nil
		}
		reason := strings.TrimSpace(response.Reason)
		if reason == "" {
			reason = "Denied by user"
		}
		return mustRawJSON(map[string]any{
			"behavior":  "deny",
			"message":   reason,
			"interrupt": false,
		}), nil
	case "hook_callback":
		decision := "allow"
		if response.Decision == executor.ControlDecisionDeny {
			decision = "deny"
		}
		reason := strings.TrimSpace(response.Reason)
		if reason == "" {
			if decision == "allow" {
				reason = "Approved by API"
			} else {
				reason = "Denied by user"
			}
		}
		return mustRawJSON(map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       decision,
				"permissionDecisionReason": reason,
			},
		}), nil
	default:
		return nil, fmt.Errorf("unsupported control subtype: %s", req.Subtype)
	}
}

func parseJSONFromLine(line string) (map[string]any, bool) {
	start := strings.Index(line, "{")
	if start < 0 {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(line[start:]), &out); err != nil {
		return nil, false
	}
	return out, true
}

func mustRawJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// Factory creates Qwen Code executor instances
type Factory struct{}

func NewFactory() *Factory {
	return &Factory{}
}

func (f *Factory) Create() (executor.Executor, error) {
	return NewClient(), nil
}
