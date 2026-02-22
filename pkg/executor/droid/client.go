// Package droid implements executor.Executor for Droid, a CLI coding agent.
//
// Droid is invoked as:
//
//	droid exec --output-format stream-json [--skip-permissions-unsafe | --auto LEVEL]
//	           [--model M] [--reasoning-effort E]
//
// The user prompt is passed via stdin (written then closed). Droid streams
// newline-delimited JSON events on stdout until it exits.
//
// Session resumption is supported via the --session-id flag.
package droid

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/supremeagent/executor/pkg/executor"
)

// Client implements executor.Executor for the Droid agent.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	logsChan  chan executor.Log
	doneChan  chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	closed    bool

	// commandRun is substituted during tests to avoid spawning real processes.
	commandRun func(name string, arg ...string) *exec.Cmd
}

// NewClient creates a new Droid executor client.
// commandRun may be nil (defaults to exec.Command) and is replaced in tests.
func NewClient(commandRun func(string, ...string) *exec.Cmd) *Client {
	if commandRun == nil {
		commandRun = exec.Command
	}
	return &Client{
		logsChan:   make(chan executor.Log, 200),
		doneChan:   make(chan struct{}),
		commandRun: commandRun,
	}
}

// Start builds the Droid CLI argument vector, spawns the process, pipes the
// prompt into stdin, and begins streaming events from stdout.
func (c *Client) Start(_ context.Context, prompt string, opts executor.Options) error {
	args := buildArgs(opts)
	return c.launch(prompt, opts.WorkingDir, opts.Env, args)
}

// launch is the shared implementation used for both initial start and test injection.
func (c *Client) launch(prompt, workingDir string, env map[string]string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("droid: no command args provided")
	}

	program := args[0]
	rest := args[1:]

	cmd := c.commandRun(program, rest...)
	cmd.Dir = workingDir
	cmd.Env = executor.BuildCommandEnv(env, map[string]string{
		"NPM_CONFIG_LOGLEVEL": "error",
	})

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("droid: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("droid: stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("droid: stderr pipe: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout

	c.sendLog(executor.Log{
		Type:    "command",
		Content: fmt.Sprintf("%s %s", program, strings.Join(rest, " ")),
	})

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("droid: start process: %w", err)
	}

	// Drain stderr in background; forward lines as error logs.
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				c.sendLog(executor.Log{Type: "stderr", Content: line})
			}
		}
	}()

	// Stream stdout droid events in background.
	go c.readLoop(stdout)

	// Write the prompt to stdin and close it so Droid knows the input is complete.
	go func() {
		defer stdin.Close()
		if _, err := fmt.Fprint(stdin, prompt); err != nil {
			c.sendLog(executor.Log{
				Type:    "error",
				Content: fmt.Sprintf("droid: write prompt: %v", err),
			})
		}
	}()

	return nil
}

// buildArgs constructs the Droid CLI argument list from executor Options.
func buildArgs(opts executor.Options) []string {
	args := []string{"droid", "exec", "--output-format", "stream-json"}

	// Map autonomy / yolo settings to CLI flags.
	switch Autonomy(opts.DroidAutonomy) {
	case AutonomyLow:
		args = append(args, "--auto", "low")
	case AutonomyMedium:
		args = append(args, "--auto", "medium")
	case AutonomyHigh:
		args = append(args, "--auto", "high")
	case AutonomySkipPermissionsUnsafe, "":
		// Default: skip all permission prompts if Yolo is set or autonomy is empty.
		if opts.Yolo || opts.DroidAutonomy == "" || opts.DroidAutonomy == string(AutonomySkipPermissionsUnsafe) {
			args = append(args, "--skip-permissions-unsafe")
		}
	case AutonomyNormal:
		// No extra flags; Droid operates with default prompts.
	}

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.DroidReasoningEffort != "" {
		args = append(args, "--reasoning-effort", opts.DroidReasoningEffort)
	}

	// Resume an existing session when ResumeSessionID is provided.
	if opts.ResumeSessionID != "" {
		args = append(args, "--session-id", opts.ResumeSessionID)
	}

	args = append(args, opts.ExtraArgs...)
	return args
}

// readLoop reads stream-json lines from r until EOF.
func (c *Client) readLoop(r io.Reader) {
	defer c.Close()
	defer c.sendLog(executor.Log{Type: "done", Content: "Droid execution finished"})

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var evt DroidEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			// Forward unparsed lines verbatim.
			c.sendLog(executor.Log{Type: "stdout", Content: line})
			continue
		}

		c.dispatchEvent(evt)
	}
}

// dispatchEvent converts a parsed DroidEvent to an executor.Log.
func (c *Client) dispatchEvent(evt DroidEvent) {
	switch evt.Type {
	case EventTypeSystem:
		c.sendLog(executor.Log{Type: "droid_system", Content: evt})
	case EventTypeMessage:
		c.sendLog(executor.Log{Type: "droid_message", Content: evt})
	case EventTypeToolCall:
		c.sendLog(executor.Log{Type: "droid_tool_call", Content: evt})
	case EventTypeToolResult:
		c.sendLog(executor.Log{Type: "droid_tool_result", Content: evt})
	case EventTypeCompletion:
		c.sendLog(executor.Log{Type: "droid_completion", Content: evt})
	case EventTypeError:
		c.sendLog(executor.Log{Type: "error", Content: evt.Message})
	default:
		c.sendLog(executor.Log{Type: "stdout", Content: evt})
	}
}

// Interrupt sends SIGINT to the Droid subprocess.
func (c *Client) Interrupt() error {
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Signal(syscall.SIGINT)
	}
	return nil
}

// SendMessage is not supported by Droid (single-shot execution); it returns an error.
func (c *Client) SendMessage(_ context.Context, _ string) error {
	return fmt.Errorf("droid: SendMessage not supported; start a new session instead")
}

// RespondControl is not supported by Droid in ACP mode; returns an error.
func (c *Client) RespondControl(_ context.Context, _ executor.ControlResponse) error {
	return fmt.Errorf("droid: RespondControl not supported")
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
		if c.stdin != nil {
			_ = c.stdin.Close()
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

// Factory creates Droid executor instances.
type Factory struct{}

// NewFactory returns a new Droid executor factory.
func NewFactory() *Factory { return &Factory{} }

// Create returns a new Droid executor.
func (f *Factory) Create() (executor.Executor, error) {
	return NewClient(nil), nil
}
