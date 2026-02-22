# Executor SDK Integration Guide

This document is intended to help developers quickly integrate with the Executor system's HTTP API or use it as a library. Executor is a unified server application for scheduling and managing AI agents (such as Claude Code, Codex, etc.) to execute tasks. It supports real-time log streaming, session recovery, manual approval, and other advanced features.

Besides serving as a standalone HTTP API, this system can also be directly integrated into other Golang projects as a dependency (please refer to Section 5: Using as a Golang Library for details).

---

## 1. Core Concepts

- **Session**: Every task execution request generates a globally unique `session_id`. All logs, statuses, and follow-up conversations are based on this ID.
- **SSE (Server-Sent Events) Streaming**: Once a task starts, the system streams AI thoughts, tool calls, and results in real-time to the client via the SSE interface.
- **Unified Event Model**: Different underlying AI output formats vary greatly. The Executor standardizes them on the server side into a consistent `Event` and `UnifiedContent` structure, making client-side rendering easier.

---

## 2. API Interface Overview

The server provides services via standard HTTP by default. The following are the core APIs needed for integration:

| Endpoint Description | HTTP Method | Path |
| --- | --- | --- |
| Start execution task | `POST` | `/api/execute` |
| Stream task logs | `GET` | `/api/execute/{session_id}/stream` |
| Continue conversation/prompt | `POST` | `/api/execute/{session_id}/continue` |
| Interrupt running task | `POST` | `/api/execute/{session_id}/interrupt` |
| Send authorization/approval | `POST` | `/api/execute/{session_id}/control` |

---

## 3. Core Workflow and Data Structures

### 3.1 Start Task (`POST /api/execute`)

Submit a task prompt for the AI to resolve, starting a new session.

**Request Body (JSON):**

```json
{
  "prompt": "Help me write a Hello World script",
  "executor": "claude_code",
  "working_dir": "/path/to/workspace",
  "model": "claude-3-7-sonnet-20250219",
  "plan": false,
  "sandbox": "",
  "env": {
    "CUSTOM_VAR": "value"
  },
  "ask_for_approval": "never"
}
```

*Notes:*
- `prompt`: (Required) The instruction given to the AI.
- `executor`: (Required) The executor type, typically `"claude_code"` or `"codex"`.
- `working_dir`: The absolute path of the working directory for the task.
- `ask_for_approval`: Whether manual approval is required. Usually set to `"never"` by default.

**Response Body (JSON):**

```json
{
  "session_id": "8b9cad0e-72a2-4b28-8081-1f2031c5dae3",
  "status": "running"
}
```

### 3.2 Receive Streaming Messages (`GET /api/execute/{session_id}/stream`)

After starting a task, the client should immediately connect to this endpoint to receive the SSE event stream.
Supported Query Parameters:
- `?return_all=true`: If disconnected during task execution, including this parameter retrieves the complete historical events from the beginning.
- `?debug=true`: Whether to include underlying debug-level events.

**SSE Data Format:**

```text
event: <Event Type>
data: <JSON Event Object>

event: ...
```

#### 📌 Core Stream Message Structure (Event Object)

Each `data` pushed over SSE is a unified JSON object structured as follows:

```json
{
  "session_id": "8b9cad0e...",
  "executor": "claude_code",
  "seq": 1,
  "timestamp": "2023-10-01T12:00:00Z",
  "type": "progress",
  "content": {
    // Unified content details (UnifiedContent)
  }
}
```

**Outer Field Descriptions:**
- `type`: The top-level event type, which is **the most critical field for frontend routing**. Primary values include:
  - `"message"`: Standard text replies, like AI greetings or summaries.
  - `"progress"`: Process state changes (e.g., "thinking", "starting system").
  - `"tool"`: Tool-related events (starting tool call, reading file, executing bash, etc.).
  - `"approval"`: Encountered a high-risk operation requiring manual approval (e.g., executing sensitive commands).
  - `"error"`: An execution error or interruption occurred.
  - `"done"`: Indicates the current session/task is completely finished.

**Inner `content` Core Structure (UnifiedContent):**

No matter what strange format the underlying AI outputs, Executor wraps it into the following unified fields, so the integrating client only needs to care about this object:

```json
{
  "category": "tool",
  "action": "reading",
  "phase": "started",
  "summary": "Reading handler.go",
  "text": "File content read or AI output content...",
  "tool_name": "ReadTool",
  "target": "handler.go",
  "request_id": "req_12345",
  "raw": {} 
}
```

**`content` Business Fields Breakdown:**

1. **`category`:** Further refines task category. E.g., `"message"`, `"tool"`, `"progress"`, `"done"`, `"error"`, `"approval"`, `"lifecycle"`.
2. **`action`:** What is currently happening.
    - Common enums: `"thinking"`, `"reading"`, `"searching"`, `"editing"`, `"tool_running"`, `"responding"`, `"completed"`, `"failed"`, `"approval_required"`.
3. **`phase`:** Indicates what stage the current action is at.
    - Common enums: `"started"`, `"completed"`, `"requested"`, `"failed"`.
4. **`summary`:** A brief description generated by the server ready for UI display (e.g., "Querying API docs", "Thinking deeply"). Perfect for progress bars or subheadings.
5. **`text`:** Contains large blocks of markdown, detailed error info, or raw AI responses meant for display.
6. **`tool_name` & `target`:** When tools are used, `tool_name` might be `Bash`, `ViewFile`, whereas `target` refers to the related file names or search keywords (useful for card highlights on UI).
7. **`request_id`:** **CRITICAL!** When `type` is `"approval"`, this field must be extracted and used in subsequent `/control` API calls to submit user approval decisions.
8. **`raw`:** The raw underlying AI node data (used for debugging and advanced customizations).

### 3.3 Manual Approval (`POST /api/execute/{session_id}/control`)

If an event with `type: "approval"` is received in the stream, the AI is paused and waiting for user authorization. The client should prompt the user and invoke this endpoint:

**Request Body (JSON):**

```json
{
  "request_id": "req_123456", 
  "decision": "approve",       
  "reason": ""                 
}
```
*Notes:* 
- `request_id` comes from the `content.request_id` in the SSE stream above.
- `decision` can only be `"approve"` or `"deny"`.
- If denied, `reason` can tell the AI why (e.g., "Do not delete this file").

### 3.4 Append Dialog or Continue Execution (`POST /api/execute/{session_id}/continue`)

When a session is interrupted, errors need manual correction, or after `done`, the user wants further changes (e.g., "Help me change the main color of the page to blue"):

**Request Body (JSON):**

```json
{
  "message": "Change the main color to blue"
}
```
*Note: After invoking this API, the existing `/stream` connection will continue streaming new events.*

### 3.5 Interupt Task (`POST /api/execute/{session_id}/interrupt`)

Called when the client clicks the "Stop Execution" button.
After invocation, the server forcefully kills the underlying AI process, and the connected `/stream` will receive a final `error` or `done` event before closing.

---

## 4. Best Practices

1. **UI Rendering Logic:**
   - Use `content.summary` as progress titles while listening to the SSE stream.
   - When encountering `category: "tool"` with `phase: "started"`, show a loading spinner. Change to a green checkmark when a matching `phase: "completed"` arrives.
   - For large texts, read `content.text` directly and render it with Markdown.
2. **Reconnection Experience:**
   - If network disconnects, reconnecting to `/stream?return_all=true` will quickly resend the session's entire history. The frontend should perform simple deduplication and replay overwriting based on the `seq` field.

---

## 5. Using as a Golang Library

In addition to calling the API Server, you can directly import this project as a standard Go module in your own application. Use package `github.com/supremeagent/executor/pkg/sdk`.

### 5.1 Initialize Client

You can initialize the SDK Client with default config, which automatically loads built-in executor factories, memory event storage, and a stream manager:

```go
package main

import (
	"context"
	"fmt"
	"github.com/supremeagent/executor/pkg/sdk"
	"github.com/supremeagent/executor/pkg/executor"
)

func main() {
	// Initialize SDK Client
	client := sdk.New()
	defer client.Shutdown()
    
	// See below for usage
}
```

Or initialize with custom components, for instance when connecting your own persistent database or registering hooks:

```go
client := sdk.NewWithOptions(sdk.ClientOptions{
	Registry:      myCustomRegistry,
	StreamManager: myCustomStreamManager,
	EventStore:    myPersistentStore,
	Hooks: executor.Hooks{
		OnSessionStart: func(ctx context.Context, sessionID string, req executor.ExecuteRequest) {
			fmt.Println("Session Started:", sessionID)
		},
		OnSessionEnd: func(ctx context.Context, sessionID string) {
			fmt.Println("Session Ended:", sessionID)
		},
	},
})
```

### 5.2 Start and Stream Task

You must provide a `context` and use the SDK's subscription mechanism to capture all structured data emitted during execution.

```go
func runTask(client *sdk.Client) {
	ctx := context.Background()
    
	// 1. Initiate task execution request
	resp, err := client.Execute(ctx, executor.ExecuteRequest{
		Prompt:     "Help write a Hello World script",
		Executor:   executor.ExecutorClaudeCode, // "claude_code" or "codex"
		WorkingDir: "/tmp/my-project",
	})
	if err != nil {
		panic(err)
	}

	sessionID := resp.SessionID
	fmt.Printf("Started Agent session: %s\n", sessionID)

	// 2. Promptly subscribe to the event stream channel
	events, unsubscribe := client.Subscribe(sessionID, executor.SubscribeOptions{
		ReturnAll:    true,  // Retrieves historical events generated before subscribing
		IncludeDebug: false,
	})
	defer unsubscribe()

	// 3. Consume output events
	for evt := range events {
		// Parse Event struct, print type and content
		fmt.Printf("[Event:%s] %#v\n", evt.Type, evt.Content)

		// Indicates the entire task is completed
		if evt.Type == "done" || evt.Type == "error" {
			fmt.Println("Agent task ended!")
			break
		}
	}
}
```

### 5.3 Session Control (Interrupt and Resume)

You can easily pause or send follow-up messages programmatically, entirely bypassing the HTTP Server constraints.

```go
// Interrupt execution
err := client.PauseTask(sessionID)

// Send prompt text to AI to continue
err := client.ContinueTask(context.Background(), sessionID, "The color isn't bright enough, change it")

// Respond to local AI tool approval requests
err := client.RespondControl(context.Background(), sessionID, executor.ControlResponse{
	RequestID: "req_xyz123",
	Decision:  executor.ControlDecisionApprove,
})
```

### 5.4 History and Session Management

If you need to cache and display history conversations locally, or check currently running Agent sessions, use the following methods:

```go
// 1. List all sessions (sorted by latest update descending)
sessions := client.ListSessions(context.Background())
for _, s := range sessions {
	fmt.Printf("Session %s [%s]: %s\n", s.SessionID, s.Status, s.Title)
}

// 2. Check if a session is still running
isRunning := client.SessionRunning(sessionID)

// 3. Get all historical event records generated for a session
events, ok := client.GetSessionEvents(sessionID)
if ok {
	fmt.Printf("Found %d historical events\n", len(events))
}

// 4. Paginate or start fetching partial history from a specific sequence number
partialEvents, err := client.ListEvents(context.Background(), sessionID, 10 /* afterSeq */, 50 /* limit */)
```

With this SDK API, not only can you quickly drive powerful AI execution capabilities, but you can seamlessly embed the entire intermediate process into your product UI!
