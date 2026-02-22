package copilot

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/supremeagent/executor/pkg/executor"
)

// Client implements the Executor interface for Copilot CLI
type Client struct {
	cmd        *exec.Cmd
	ptyFile    *os.File
	logsChan   chan executor.Log
	doneChan   chan struct{}
	closeOnce  sync.Once
	closed     bool
	mu         sync.Mutex
	commandRun func(name string, arg ...string) *exec.Cmd
}

// NewClient creates a new Copilot Code client
func NewClient() *Client {
	return &Client{
		logsChan:   make(chan executor.Log, 100),
		doneChan:   make(chan struct{}),
		commandRun: exec.Command,
	}
}

// Start starts the Copilot Code executor with the given prompt
func (c *Client) Start(ctx context.Context, prompt string, opts executor.Options) error {
	args := []string{"-y", "--package", "@github/copilot@latest", "copilot"}

	args = append(args, "-p", prompt)

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	if opts.CopilotAllowAllTools || opts.Yolo || opts.DangerouslySkipPermissions {
		args = append(args, "--allow-all-tools")
	}

	cmd := c.commandRun("npx", args...)
	cmd.Dir = opts.WorkingDir
	cmd.Env = executor.BuildCommandEnv(opts.Env, map[string]string{
		"NPM_CONFIG_LOGLEVEL": "error",
		"NODE_NO_WARNINGS":    "1",
		"CI":                  "1",
		"TERM":                "dumb",
		"NO_COLOR":            "1", // strip ansi code
	})

	c.sendLog(executor.Log{Type: "command", Content: fmt.Sprintf("npx %s", strings.Join(args, " "))})

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start command with pty: %w", err)
	}

	c.cmd = cmd
	c.ptyFile = ptmx

	go func() {
		defer c.Close()
		defer ptmx.Close()
		defer c.sendLog(executor.Log{Type: "done", Content: "Copilot execution finished"})

		scanner := bufio.NewScanner(ptmx)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.sendLog(executor.Log{Type: "stdout", Content: line})
		}

		if err := cmd.Wait(); err != nil {
			c.sendLog(executor.Log{Type: "error", Content: err.Error()})
		}
	}()

	return nil
}

func (c *Client) Interrupt() error {
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) SendMessage(ctx context.Context, message string) error {
	return fmt.Errorf("copilot does not support interactive sending in stream mode")
}

func (c *Client) RespondControl(ctx context.Context, response executor.ControlResponse) error {
	return fmt.Errorf("copilot does not support interactive control in stream mode")
}

func (c *Client) Wait() error {
	<-c.doneChan
	return nil
}

func (c *Client) Logs() <-chan executor.Log {
	return c.logsChan
}

func (c *Client) Done() <-chan struct{} {
	return c.doneChan
}

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
			c.ptyFile.Close()
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

type Factory struct{}

func NewFactory() *Factory {
	return &Factory{}
}

func (f *Factory) Create() (executor.Executor, error) {
	return NewClient(), nil
}
