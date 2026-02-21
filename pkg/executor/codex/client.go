package codex

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
	"time"

	"github.com/supremeagent/executor/pkg/executor"
)

// Client implements the Executor interface for Codex
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	logsChan  chan executor.Log
	doneChan  chan struct{}
	closeOnce sync.Once
	closed    bool
	mu        sync.Mutex

	conversationID string
	autoApprove    bool

	pendingMu sync.Mutex
	pending   map[int64]chan JSONRPCMessage

	commandRun func(name string, arg ...string) *exec.Cmd
	idCounter  int64
}

// NewClient creates a new Codex client
func NewClient() *Client {
	return &Client{
		logsChan:   make(chan executor.Log, 100),
		doneChan:   make(chan struct{}),
		pending:    make(map[int64]chan JSONRPCMessage),
		commandRun: exec.Command,
		idCounter:  1,
	}
}

func (c *Client) nextID() RequestID {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.idCounter++
	id := c.idCounter
	return RequestID{Number: &id}
}

// Start starts the Codex executor with the given prompt
func (c *Client) Start(ctx context.Context, prompt string, opts executor.Options) error {
	// Build command for Codex app-server
	args := []string{"-y", "@openai/codex@0.104.0", "app-server", "--listen", "stdio://"}

	cmd := c.commandRun("npx", args...)
	cmd.Dir = opts.WorkingDir
	cmd.Env = executor.BuildCommandEnv(opts.Env)

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
	c.stdout = stdout

	// Log the command being executed
	c.sendLog(executor.Log{Type: "init", Content: fmt.Sprintf("npx %s", strings.Join(args, " "))})

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start codex: %w", err)
	}

	// Handle stderr in background
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			c.sendLog(executor.Log{Type: "error", Content: line})
		}
	}()

	// Determine auto-approve setting
	c.autoApprove = opts.AskForApproval == "never" || opts.AskForApproval == ""

	// Start reading responses in background
	go c.readLoop(ctx, stdout)

	// Run initialization and starting in a goroutine
	go func() {
		sendError := func(msg string) {
			c.sendLog(executor.Log{Type: "error", Content: msg})
			c.Close()
		}

		// Initialize the connection
		if err := c.initialize(); err != nil {
			sendError(fmt.Sprintf("failed to initialize: %v", err))
			return
		}

		// Start a new conversation
		conversationID, err := c.newConversation(opts)
		if err != nil {
			sendError(fmt.Sprintf("failed to create conversation: %v", err))
			return
		}
		c.conversationID = conversationID

		// Add conversation listener
		if err := c.addListener(conversationID); err != nil {
			sendError(fmt.Sprintf("failed to add listener: %v", err))
			return
		}

		// Send the user message
		if err := c.sendUserMessage(conversationID, prompt); err != nil {
			sendError(fmt.Sprintf("failed to send message: %v", err))
			return
		}
	}()

	return nil
}

func (c *Client) initialize() error {
	params := InitializeParams{
		ClientInfo: ClientInfo{
			Name:    "SupremeAgent",
			Version: "1.0.0",
		},
		Capabilities:    map[string]any{},
		ProtocolVersion: "2025-06-18",
	}

	req := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      ptrToRequestID(c.nextID()),
		Method:  "initialize",
		Params:  mustJSON(params),
	}

	// Wait for initialize response before continuing
	if _, err := c.sendRequest(req); err != nil {
		return err
	}

	// Send initialized notification
	notif := JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  "initialized",
		Params:  mustJSON(map[string]any{}),
	}
	if err := c.sendJSON(notif); err != nil {
		return err
	}

	return nil
}

func (c *Client) newConversation(opts executor.Options) (string, error) {
	sandbox := opts.Sandbox
	if sandbox == "" {
		sandbox = "workspace-write"
	}

	askForApproval := opts.AskForApproval
	if askForApproval == "" {
		askForApproval = "unless-trusted"
	}

	params := NewConversationParams{
		Model:                opts.Model,
		Sandbox:              sandbox,
		AskForApproval:       askForApproval,
		ModelReasoningEffort: opts.ModelReasoningEffort,
		WorkingDirectory:     opts.WorkingDir,
	}

	req := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      ptrToRequestID(c.nextID()),
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
		ID:      ptrToRequestID(c.nextID()),
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
			{
				Type: "text",
				Data: InputItemData{
					Text: content,
				},
			},
		},
	}

	req := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      ptrToRequestID(c.nextID()),
		Method:  "sendUserMessage",
		Params:  mustJSON(params),
	}

	_, err := c.sendRequest(req)
	return err
}

// Interrupt interrupts the current execution
func (c *Client) Interrupt() error {
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Signal(syscall.SIGINT)
	}
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
		c.mu.Lock()
		c.closed = true
		close(c.logsChan)
		close(c.doneChan)
		c.mu.Unlock()

		if c.cmd != nil && c.cmd.Process != nil {
			c.cmd.Process.Kill()
			c.cmd.Wait()
		}

		if c.stdin != nil {
			c.stdin.Close()
		}

		c.pendingMu.Lock()
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = nil
		c.pendingMu.Unlock()
	})

	return nil
}

func (c *Client) sendLog(log executor.Log) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.logsChan <- log
}

func (c *Client) sendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	c.pendingMu.Lock()
	if c.stdin == nil {
		c.pendingMu.Unlock()
		return fmt.Errorf("stdin is nil")
	}
	_, err = c.stdin.Write(data)
	if err != nil {
		c.pendingMu.Unlock()
		return fmt.Errorf("failed to write to stdin: %w", err)
	}
	_, err = c.stdin.Write([]byte("\n"))
	c.pendingMu.Unlock()

	if err != nil {
		return fmt.Errorf("failed to write newline to stdin: %w", err)
	}

	return nil
}

func (c *Client) sendRequest(req JSONRPCMessage) (JSONRPCMessage, error) {
	if req.ID == nil {
		return JSONRPCMessage{}, fmt.Errorf("request ID is nil")
	}

	id := *req.ID.Number
	ch := make(chan JSONRPCMessage, 1)

	c.pendingMu.Lock()
	if c.pending == nil {
		c.pendingMu.Unlock()
		return JSONRPCMessage{}, fmt.Errorf("client closed")
	}
	c.pending[id] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		if c.pending != nil {
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()
	}()

	if err := c.sendJSON(req); err != nil {
		return JSONRPCMessage{}, err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return JSONRPCMessage{}, fmt.Errorf("channel closed")
		}
		if resp.Error != nil {
			return JSONRPCMessage{}, fmt.Errorf("RPC error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
		}
		return resp, nil
	case <-time.After(60 * time.Second):
		return JSONRPCMessage{}, fmt.Errorf("timeout waiting for response")
	}
}

func ctxDone(ctx context.Context) <-chan struct{} {
	return ctx.Done()
}

func (c *Client) readLoop(_ context.Context, stdout io.Reader) {
	defer c.Close()
	defer c.sendLog(executor.Log{Type: "done", Content: "Codex execution finished"})

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Try to parse as JSON-RPC message
		var msg JSONRPCMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			c.sendLog(executor.Log{Type: "error", Content: line})
			continue
		}

		// If it's a response, send to pending request
		if msg.ID != nil && msg.ID.Number != nil {
			id := *msg.ID.Number
			c.pendingMu.Lock()
			if ch, ok := c.pending[id]; ok {
				ch <- msg
			}
			c.pendingMu.Unlock()
		}

		// Always log the message
		if msg.Method == "" {
			c.sendLog(executor.Log{Type: "output", Content: line})
		} else {
			// Check for events
			c.sendLog(executor.Log{Type: msg.Method, Content: msg.Params})

			// Check for task completion
			if msg.Method == "codex/event/task_complete" {
				return
			}
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

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
