package mcpapi

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/sdk"
)

// NewExecutorServer builds an MCP Server with all executor API tools registered.
func NewExecutorServer(client *sdk.Client) *Server {
	return NewExecutorServerIO(client, os.Stdin, os.Stdout)
}

// NewExecutorServerIO builds an MCP Server using the provided reader/writer.
func NewExecutorServerIO(client *sdk.Client, in io.Reader, out io.Writer) *Server {
	srv := NewServer(in, out)
	registerTools(srv, client)
	return srv
}

func registerTools(srv *Server, client *sdk.Client) {
	// execute
	srv.RegisterTool(Tool{
		Name:        "execute",
		Description: "Start a new AI executor task. Returns a session_id to track the task.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]jsonSchema{
				"prompt":           {Type: "string", Description: "The task prompt to send to the executor (required)."},
				"executor":         {Type: "string", Description: "Executor to use.", Enum: []string{"claude_code", "codex", "qwen", "droid", "copilot", "gemini"}},
				"working_dir":      {Type: "string", Description: "Working directory for the task."},
				"model":            {Type: "string", Description: "Model override for the executor."},
				"plan":             {Type: "boolean", Description: "Run in plan mode (approval required for actions)."},
				"sandbox":          {Type: "string", Description: "Sandbox mode (executor-specific)."},
				"ask_for_approval": {Type: "string", Description: "When to ask for approval: never, always, or a specific event type."},
			},
			Required: []string{"prompt"},
		},
	}, func(ctx context.Context, args map[string]any) (ToolResult, error) {
		req := executor.ExecuteRequest{
			Prompt:     stringArg(args, "prompt"),
			WorkingDir: stringArg(args, "working_dir"),
			Model:      stringArg(args, "model"),
			Sandbox:    stringArg(args, "sandbox"),
			AskForApproval: stringArg(args, "ask_for_approval"),
		}
		if ex := stringArg(args, "executor"); ex != "" {
			req.Executor = executor.ExecutorType(ex)
		}
		if plan, ok := args["plan"].(bool); ok {
			req.Plan = plan
		}
		resp, err := client.Execute(ctx, req)
		if err != nil {
			return ErrorResult(err), nil
		}
		return TextResult(resp), nil
	})

	// continue_task
	srv.RegisterTool(Tool{
		Name:        "continue_task",
		Description: "Send a follow-up message to a running or paused executor session.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]jsonSchema{
				"session_id": {Type: "string", Description: "Session ID to continue."},
				"message":    {Type: "string", Description: "Message to send to the executor."},
			},
			Required: []string{"session_id"},
		},
	}, func(ctx context.Context, args map[string]any) (ToolResult, error) {
		sessionID := stringArg(args, "session_id")
		if sessionID == "" {
			return ErrorResult(fmt.Errorf("session_id is required")), nil
		}
		message := stringArg(args, "message")
		if err := client.ContinueTask(ctx, sessionID, message); err != nil {
			return ErrorResult(err), nil
		}
		return TextResult(map[string]string{"status": "ok"}), nil
	})

	// interrupt_task
	srv.RegisterTool(Tool{
		Name:        "interrupt_task",
		Description: "Interrupt (pause) a running executor session.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]jsonSchema{
				"session_id": {Type: "string", Description: "Session ID to interrupt."},
			},
			Required: []string{"session_id"},
		},
	}, func(_ context.Context, args map[string]any) (ToolResult, error) {
		sessionID := stringArg(args, "session_id")
		if sessionID == "" {
			return ErrorResult(fmt.Errorf("session_id is required")), nil
		}
		if err := client.PauseTask(sessionID); err != nil {
			return ErrorResult(err), nil
		}
		return TextResult(map[string]string{"status": "interrupted"}), nil
	})

	// control_task
	srv.RegisterTool(Tool{
		Name:        "control_task",
		Description: "Respond to an executor control/approval request (approve or deny).",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]jsonSchema{
				"session_id": {Type: "string", Description: "Session ID of the task."},
				"request_id": {Type: "string", Description: "Control request ID to respond to."},
				"decision":   {Type: "string", Description: "Decision: approve or deny.", Enum: []string{"approve", "deny"}},
				"reason":     {Type: "string", Description: "Optional reason for the decision."},
			},
			Required: []string{"session_id", "request_id", "decision"},
		},
	}, func(ctx context.Context, args map[string]any) (ToolResult, error) {
		sessionID := stringArg(args, "session_id")
		requestID := stringArg(args, "request_id")
		decision := stringArg(args, "decision")
		if sessionID == "" || requestID == "" || decision == "" {
			return ErrorResult(fmt.Errorf("session_id, request_id, and decision are required")), nil
		}
		if decision != string(executor.ControlDecisionApprove) && decision != string(executor.ControlDecisionDeny) {
			return ErrorResult(fmt.Errorf("decision must be approve or deny")), nil
		}
		resp := executor.ControlResponse{
			RequestID: requestID,
			Decision:  executor.ControlDecision(decision),
			Reason:    stringArg(args, "reason"),
		}
		if err := client.RespondControl(ctx, sessionID, resp); err != nil {
			return ErrorResult(err), nil
		}
		return TextResult(map[string]string{"status": "ok"}), nil
	})

	// get_events
	srv.RegisterTool(Tool{
		Name:        "get_events",
		Description: "Retrieve stored events for an executor session (polling alternative to streaming).",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]jsonSchema{
				"session_id": {Type: "string", Description: "Session ID to fetch events for."},
				"after_seq":  {Type: "number", Description: "Return only events with seq > after_seq."},
				"limit":      {Type: "number", Description: "Maximum number of events to return (0 = no limit)."},
			},
			Required: []string{"session_id"},
		},
	}, func(ctx context.Context, args map[string]any) (ToolResult, error) {
		sessionID := stringArg(args, "session_id")
		if sessionID == "" {
			return ErrorResult(fmt.Errorf("session_id is required")), nil
		}
		afterSeq := uint64(numberArg(args, "after_seq"))
		limit := int(numberArg(args, "limit"))
		events, err := client.ListEvents(ctx, sessionID, afterSeq, limit)
		if err != nil {
			return ErrorResult(err), nil
		}
		return TextResult(map[string]any{
			"session_id": sessionID,
			"events":     events,
		}), nil
	})

	// list_sessions
	srv.RegisterTool(Tool{
		Name:        "list_sessions",
		Description: "List all known executor sessions.",
		InputSchema: jsonSchema{Type: "object", Properties: map[string]jsonSchema{}},
	}, func(ctx context.Context, _ map[string]any) (ToolResult, error) {
		sessions := client.ListSessions(ctx)
		return TextResult(map[string]any{"sessions": sessions}), nil
	})

	// list_executors
	srv.RegisterTool(Tool{
		Name:        "list_executors",
		Description: "List all available executor types.",
		InputSchema: jsonSchema{Type: "object", Properties: map[string]jsonSchema{}},
	}, func(_ context.Context, _ map[string]any) (ToolResult, error) {
		executors := client.Executors()
		return TextResult(map[string]any{"executors": executors}), nil
	})
}

func stringArg(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func numberArg(args map[string]any, key string) float64 {
	if v, ok := args[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}
