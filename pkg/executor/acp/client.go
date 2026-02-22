package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/supremeagent/executor/pkg/executor"
)

// Client implements executor.Executor for ACP-based tools (Gemini, Qwen, Copilot).
//
// These tools communicate over a line-delimited JSON protocol:
//   - The user prompt is written to stdin followed by a newline.
//   - The tool streams ACP events on stdout until it exits.
//   - Approval/permission requests are forwarded via control_request logs.
//
// commandRun may be set to a test double to avoid spawning real processes.
type Client struct {
	// args is the full argument vector: args[0] is the program, args[1:] are flags.
	args []string

	cmd     *exec.Cmd
	ptyFile *os.File
	stdin   io.WriteCloser

	logsChan  chan executor.Log
	doneChan  chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	closed    bool

	// pending tracks unanswered permission requests keyed by tool_call_id.
	pending   map[string]struct{}
	pendingMu sync.Mutex

	// autoApprove, when true, immediately approves every incoming permission request.
	autoApprove bool

	// commandRun is substituted during tests to avoid spawning real processes.
	commandRun func(name string, arg ...string) *exec.Cmd
}

// NewClientWithArgs creates an ACP client that will invoke args[0] with args[1:].
// commandRun defaults to exec.Command when nil.
func NewClientWithArgs(commandRun func(string, ...string) *exec.Cmd, args []string) *Client {
	if commandRun == nil {
		commandRun = exec.Command
	}
	return &Client{
		args:       args,
		logsChan:   make(chan executor.Log, 200),
		doneChan:   make(chan struct{}),
		pending:    make(map[string]struct{}),
		commandRun: commandRun,
	}
}

// NewClient creates a client with args not yet set. Call Start with a populated Options.
func NewClient(commandRun func(string, ...string) *exec.Cmd) *Client {
	return NewClientWithArgs(commandRun, nil)
}

// SetAutoApprove configures whether the client automatically approves all
// permission requests (yolo / dangerously-skip-permissions mode).
func (c *Client) SetAutoApprove(v bool) {
	c.autoApprove = v
}

// Start launches the ACP tool process, writes the initial prompt to stdin, and
// begins streaming events from stdout. It returns immediately; call Logs() to
// receive events.
func (c *Client) Start(_ context.Context, prompt string, opts executor.Options) error {
	if len(c.args) == 0 {
		return fmt.Errorf("acp: no command args provided")
	}

	program := c.args[0]
	rest := c.args[1:]

	cmd := c.commandRun(program, rest...)
	cmd.Dir = opts.WorkingDir
	cmd.Env = executor.BuildCommandEnv(opts.Env, map[string]string{
		"NPM_CONFIG_LOGLEVEL": "error",
		"NODE_NO_WARNINGS":    "1",
		"CI":                  "1",
		"TERM":                "dumb",
		"NO_COLOR":            "1",
	})

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("acp: start process with pty: %w", err)
	}

	c.cmd = cmd
	c.ptyFile = ptmx
	c.stdin = ptmx

	c.sendLog(executor.Log{
		Type:    "command",
		Content: fmt.Sprintf("%s %s", program, strings.Join(rest, " ")),
	})

	// Stream stdout ACP events from the PTY.
	go c.readLoop(ptmx)

	// Deliver the user prompt via stdin
	go func() {
		payload := map[string]any{
			"type":    "user_message",
			"content": prompt,
		}
		data, _ := json.Marshal(payload)
		// Do not close ptmx here! Otherwise the entire shell dies and we lose events.
		// ACP relies on continuous interaction.
		if _, err := fmt.Fprintf(ptmx, "%s\n", data); err != nil {
			c.sendLog(executor.Log{
				Type:    "error",
				Content: fmt.Sprintf("acp: write prompt: %v", err),
			})
		}
	}()

	return nil
}

// readLoop reads ACP event lines from r until EOF and translates them to executor.Log entries.
func (c *Client) readLoop(r io.Reader) {
	defer c.Close()
	defer c.sendLog(executor.Log{Type: "done", Content: "ACP execution finished"})

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		raw := []byte(line)
		evt, ok := parseEvent(raw)
		if !ok {
			// Emit non-ACP lines verbatim (startup messages, etc.).
			c.sendLog(executor.Log{Type: "stdout", Content: line})
			continue
		}

		c.dispatchEvent(evt, raw)
	}
}

// dispatchEvent converts an ACP event to an executor.Log and sends it.
func (c *Client) dispatchEvent(evt Event, raw []byte) {
	switch evt.Type {
	case EventTypeDone:
		// Emitted by the harness when the prompt loop finishes. We just forward it.
		c.sendLog(executor.Log{Type: "acp_done", Content: json.RawMessage(raw)})

	case EventTypeError:
		var msg string
		_ = json.Unmarshal(evt.Raw, &msg)
		c.sendLog(executor.Log{Type: "error", Content: msg})

	case EventTypeSessionStart:
		var sessionID string
		_ = json.Unmarshal(evt.Raw, &sessionID)
		c.sendLog(executor.Log{Type: "session_start", Content: sessionID})

	case EventTypeApprovalRequest:
		var perm PermissionRequest
		if err := json.Unmarshal(evt.Raw, &perm); err == nil {
			c.pendingMu.Lock()
			c.pending[perm.ToolCallID] = struct{}{}
			c.pendingMu.Unlock()

			c.sendLog(executor.Log{
				Type: "control_request",
				Content: map[string]any{
					"request_id": perm.ToolCallID,
					"tool_call":  perm.ToolCall,
				},
			})

			if c.autoApprove {
				_ = c.approveToolCall(perm.ToolCallID, true, "")
			}
		}

	default:
		c.sendLog(executor.Log{Type: string(evt.Type), Content: json.RawMessage(raw)})
	}
}

// approveToolCall writes an approval or denial response to stdin.
func (c *Client) approveToolCall(toolCallID string, allow bool, reason string) error {
	decision := "allow"
	if !allow {
		decision = "deny"
	}
	payload := map[string]any{
		"type":         "approval_response",
		"tool_call_id": toolCallID,
		"decision":     decision,
	}
	if reason != "" {
		payload["reason"] = reason
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.stdin == nil {
		return executor.ErrExecutorClosed
	}
	_, err = fmt.Fprintf(c.stdin, "%s\n", data)
	return err
}

// Interrupt sends SIGINT to the subprocess.
func (c *Client) Interrupt() error {
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Signal(syscall.SIGINT)
	}
	return nil
}

// SendMessage sends a follow-up message to the running session via stdin.
func (c *Client) SendMessage(_ context.Context, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.stdin == nil {
		return executor.ErrExecutorClosed
	}
	payload := map[string]any{
		"type":    "user_message",
		"content": message,
	}
	data, _ := json.Marshal(payload)
	_, err := fmt.Fprintf(c.stdin, "%s\n", data)
	return err
}

// RespondControl answers a pending approval request identified by RequestID.
func (c *Client) RespondControl(_ context.Context, response executor.ControlResponse) error {
	c.pendingMu.Lock()
	_, ok := c.pending[response.RequestID]
	if ok {
		delete(c.pending, response.RequestID)
	}
	c.pendingMu.Unlock()

	if !ok {
		return fmt.Errorf("acp: control request %q not found", response.RequestID)
	}

	allow := response.Decision == executor.ControlDecisionApprove
	return c.approveToolCall(response.RequestID, allow, response.Reason)
}

// Wait blocks until the executor finishes.
func (c *Client) Wait() error {
	<-c.doneChan
	return nil
}

// Logs returns the channel of streaming log entries.
func (c *Client) Logs() <-chan executor.Log {
	return c.logsChan
}

// Done returns a channel closed when execution completes.
func (c *Client) Done() <-chan struct{} {
	return c.doneChan
}

// Close terminates the subprocess and frees resources. Safe to call multiple times.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.logsChan)
		close(c.doneChan)
		c.mu.Unlock()

		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
			_ = c.cmd.Wait()
		}
		if c.ptyFile != nil {
			_ = c.ptyFile.Close()
		}
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
