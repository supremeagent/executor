// Package gemini implements executor.Executor for Google Gemini CLI.
//
// Gemini CLI speaks the Agent Client Protocol (ACP) over stdin/stdout using
// line-delimited JSON. The binary is invoked via npx:
//
//	npx -y @google/gemini-cli@latest --experimental-acp [--yolo] [--model M]
//
// When --yolo is set the CLI approves all tool-use requests automatically so
// no approval handshake is needed from the user.
package gemini

import (
	"context"
	"os/exec"

	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/acp"
)

const (
	// npmPackage is the npm package used to invoke Gemini CLI.
	npmPackage = "@google/gemini-cli@latest"
)

// Client is a thin wrapper around acp.Client that builds the Gemini-specific
// command-line arguments before delegating to the shared ACP harness.
type Client struct {
	inner      *acp.Client
	commandRun func(string, ...string) *exec.Cmd
}

// NewClient creates a new Gemini executor client.
// commandRun may be nil (defaults to exec.Command) and is replaced in tests.
func NewClient(commandRun func(string, ...string) *exec.Cmd) *Client {
	return &Client{commandRun: commandRun}
}

// Start builds the Gemini CLI argument vector and launches the process.
func (c *Client) Start(ctx context.Context, prompt string, opts executor.Options) error {
	args := buildArgs(opts)
	inner := acp.NewClientWithArgs(c.commandRun, args)
	inner.SetAutoApprove(opts.Yolo)
	c.inner = inner
	return inner.Start(ctx, prompt, opts)
}

// buildArgs constructs the npx argument list for Gemini CLI.
func buildArgs(opts executor.Options) []string {
	args := []string{"npx", "-y", npmPackage}

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Yolo {
		// --yolo enables auto-approval; --allowed-tools restricts shell access.
		args = append(args, "--yolo", "--allowed-tools", "run_shell_command")
	}

	// Required flag to enable ACP mode.
	args = append(args, "--experimental-acp")
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

// Factory creates Gemini executor instances.
type Factory struct{}

// NewFactory returns a new Gemini executor factory.
func NewFactory() *Factory { return &Factory{} }

// Create returns a new Gemini executor.
func (f *Factory) Create() (executor.Executor, error) {
	return NewClient(nil), nil
}
