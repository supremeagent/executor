// Package copilot implements executor.Executor for GitHub Copilot CLI.
//
// Copilot CLI speaks the Agent Client Protocol (ACP) over stdin/stdout using
// line-delimited JSON. The binary is invoked via npx:
//
//	npx -y @github/copilot@latest --acp [options]
//
// Available options include:
//   - --model M           : select the underlying model
//   - --allow-all-tools   : skip tool-use approval prompts
//   - --allow-tool T      : allow a specific tool
//   - --deny-tool T       : deny a specific tool
//   - --add-dir D         : add an extra directory to the workspace
//   - --disable-mcp-server S : disable a named MCP server
package copilot

import (
	"context"
	"os/exec"

	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/acp"
)

const (
	// npmPackage is the npm package used to invoke GitHub Copilot CLI.
	npmPackage = "@github/copilot@latest"
)

// Client is a thin wrapper around acp.Client that builds the Copilot-specific
// command-line arguments before delegating to the shared ACP harness.
type Client struct {
	inner      *acp.Client
	commandRun func(string, ...string) *exec.Cmd
}

// NewClient creates a new Copilot executor client.
// commandRun may be nil (defaults to exec.Command) and is replaced in tests.
func NewClient(commandRun func(string, ...string) *exec.Cmd) *Client {
	return &Client{commandRun: commandRun}
}

// Start builds the Copilot CLI argument vector and launches the process.
func (c *Client) Start(ctx context.Context, prompt string, opts executor.Options) error {
	args := buildArgs(opts)
	inner := acp.NewClientWithArgs(c.commandRun, args)
	// When all tools are allowed, auto-approve approval requests.
	inner.SetAutoApprove(opts.CopilotAllowAllTools)
	c.inner = inner
	return inner.Start(ctx, prompt, opts)
}

// buildArgs constructs the npx argument list for Copilot CLI.
func buildArgs(opts executor.Options) []string {
	args := []string{"npx", "-y", npmPackage}

	if opts.CopilotAllowAllTools {
		args = append(args, "--allow-all-tools")
	}

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}

	// Required flag to enable ACP mode.
	args = append(args, "--acp")
	args = append(args, opts.ExtraArgs...)
	return args
}

func (c *Client) Interrupt() error {
	if c.inner != nil {
		return c.inner.Interrupt()
	}
	return nil
}

func (c *Client) SendMessage(ctx context.Context, message string) error {
	if c.inner != nil {
		return c.inner.SendMessage(ctx, message)
	}
	return executor.ErrExecutorClosed
}

func (c *Client) RespondControl(ctx context.Context, response executor.ControlResponse) error {
	if c.inner != nil {
		return c.inner.RespondControl(ctx, response)
	}
	return executor.ErrExecutorClosed
}

func (c *Client) Wait() error {
	if c.inner != nil {
		return c.inner.Wait()
	}
	return nil
}

func (c *Client) Logs() <-chan executor.Log {
	if c.inner != nil {
		return c.inner.Logs()
	}
	ch := make(chan executor.Log)
	close(ch)
	return ch
}

func (c *Client) Done() <-chan struct{} {
	if c.inner != nil {
		return c.inner.Done()
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (c *Client) Close() error {
	if c.inner != nil {
		return c.inner.Close()
	}
	return nil
}

// Factory creates Copilot executor instances.
type Factory struct{}

// NewFactory returns a new Copilot executor factory.
func NewFactory() *Factory { return &Factory{} }

// Create returns a new Copilot executor.
func (f *Factory) Create() (executor.Executor, error) {
	return NewClient(nil), nil
}
