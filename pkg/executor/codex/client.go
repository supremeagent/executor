package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"sync"

	"github.com/anthropics/vibe-kanban/go-api/pkg/executor"
)

// Client implements the Executor interface for Codex
type Client struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser

	logsChan       chan executor.Log
	doneChan       chan struct{}
	closeOnce      sync.Once

	conversationID string
	autoApprove   bool
}

// NewClient creates a new Codex client
func NewClient() *Client {
	return &Client{
		logsChan: make(chan executor.Log, 100),
		doneChan: make(chan struct{}),
	}
}

// Start starts the Codex executor with the given prompt
func (c *Client) Start(ctx context.Context, prompt string, opts executor.Options) error {
	// Build command for Codex app-server
	args := []string{"-y", "@openai/codex@0.101.0", "app-server"}

	cmd := exec.Command("npx", args...)
	cmd.Dir = opts.WorkingDir

	// Set up stdin/stdout
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start codex: %w", err)
	}

	// Handle stderr in background
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			c.logsChan <- executor.Log{Type: "stderr", Content: line}
		}
	}()

	// Determine auto-approve setting
	c.autoApprove = opts.AskForApproval == "never" || opts.AskForApproval == ""

	// Initialize the connection
	if err := c.initialize(); err != nil {
		syscall.Kill(c.cmd.Process.Pid, syscall.SIGKILL)
		return fmt.Errorf("failed to initialize: %w", err)
	}

	// Start a new conversation
	conversationID, err := c.newConversation(opts)
	if err != nil {
		syscall.Kill(c.cmd.Process.Pid, syscall.SIGKILL)
		return fmt.Errorf("failed to create conversation: %w", err)
	}
	c.conversationID = conversationID

	// Add conversation listener
	if err := c.addListener(conversationID); err != nil {
		syscall.Kill(c.cmd.Process.Pid, syscall.SIGKILL)
		return fmt.Errorf("failed to add listener: %w", err)
	}

	// Start reading responses in background
	go c.readLoop(ctx, stdout)

	// Send the user message
	if err := c.sendUserMessage(conversationID, prompt); err != nil {
		syscall.Kill(c.cmd.Process.Pid, syscall.SIGKILL)
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

func (c *Client) initialize() error {
	params := InitializeParams{
		ClientInfo: ClientInfo{
			Name:    "vibe-kanban-go-api",
			Version: "1.0.0",
		},
		Capabilities: nil,
	}

	req := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      ptrToRequestID(nextID()),
		Method:  "initialize",
		Params:  mustJSON(params),
	}

	if err := c.sendJSON(req); err != nil {
		return err
	}

	// Send initialized notification
	notif := JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  "initialized",
		Params:  nil,
	}
	if err := c.sendJSON(notif); err != nil {
		return err
	}

	return nil
}

func (c *Client) newConversation(opts executor.Options) (string, error) {
	sandbox := opts.Sandbox
	if sandbox == "" {
		sandbox = "auto"
	}

	askForApproval := opts.AskForApproval
	if askForApproval == "" {
		askForApproval = "unless-trusted"
	}

	params := NewConversationParams{
		Model:               opts.Model,
		Sandbox:             sandbox,
		AskForApproval:     askForApproval,
		ModelReasoningEffort: opts.ModelReasoningEffort,
		WorkingDirectory:   opts.WorkingDir,
	}

	req := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      ptrToRequestID(nextID()),
		Method:  "newConversation",
		Params:  mustJSON(params),
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	var result NewConversationResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("failed to parse newConversation result: %w", err)
	}

	return result.ConversationID, nil
}

func (c *Client) addListener(conversationID string) error {
	params := AddConversationListenerParams{
		ConversationID:        conversationID,
		ExperimentalRawEvents: false,
	}

	req := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      ptrToRequestID(nextID()),
		Method:  "addConversationListener",
		Params:  mustJSON(params),
	}

	_, err := c.sendRequest(req)
	return err
}

func (c *Client) sendUserMessage(conversationID, content string) error {
	params := SendUserMessageParams{
		ConversationID: conversationID,
		Items: []InputItem{
			{Type: "text", Text: content},
		},
	}

	req := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      ptrToRequestID(nextID()),
		Method:  "sendUserMessage",
		Params:  mustJSON(params),
	}

	_, err := c.sendRequest(req)
	return err
}

// Interrupt interrupts the current execution
func (c *Client) Interrupt() error {
	// Codex doesn't have an explicit interrupt, we just close the connection
	return nil
}

// SendMessage sends a message to continue the conversation
func (c *Client) SendMessage(ctx context.Context, message string) error {
	return c.sendUserMessage(c.conversationID, message)
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

	if c.cmd != nil && c.cmd.Process != nil {
		syscall.Kill(c.cmd.Process.Pid, syscall.SIGKILL)
		c.cmd.Wait()
	}

	if c.stdin != nil {
		c.stdin.Close()
	}

	return nil
}

func (c *Client) sendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	_, err = c.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}
	_, err = c.stdin.Write([]byte("\n"))
	if err != nil {
		return fmt.Errorf("failed to write newline to stdin: %w", err)
	}

	return nil
}

// pendingResponses holds pending request responses
type pendingResponses map[int64]chan JSONRPCMessage

func (c *Client) sendRequest(req JSONRPCMessage) (JSONRPCMessage, error) {
	// For now, simplified - just send and read response
	// In production, we'd need proper request/response matching
	if err := c.sendJSON(req); err != nil {
		return JSONRPCMessage{}, err
	}

	// The response will come through the read loop
	// For now, we just return success
	return JSONRPCMessage{}, nil
}

func (c *Client) readLoop(ctx context.Context, stdout io.Reader) {
	defer func() {
		c.doneChan <- struct{}{}
	}()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Log the raw message
		c.logsChan <- executor.Log{Type: "stdout", Content: line}

		// Try to parse as notification
		var msg JSONRPCMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		// Check for task completion
		if msg.Method == "codex/event/task_complete" {
			c.logsChan <- executor.Log{Type: "done", Content: line}
			return
		}
	}
}

// Factory creates Codex executor instances
type Factory struct{}

func NewFactory() *Factory {
	return &Factory{}
}

func (f *Factory) Create() (executor.Executor, error) {
	return NewClient(), nil
}

// Helper functions
func ptrToRequestID(id RequestID) *RequestID {
	return &id
}

func mustJSON(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
