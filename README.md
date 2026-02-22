# SupremeAgent Executor

The `executor` project is a powerful Go-based backend server and SDK that provides a unified interface for executing tasks using various AI coding agents. It serves as a centralized bridge to standardize the interaction with multiple AI executors—such as **Claude Code**, **Codex**, **Gemini**, **Qwen**, **Copilot**, and **Droid**—allowing you to start, monitor, and manage AI-driven code generation and file system modifications in a fully controlled environment.

## 🚀 Key Features

- **Unified Executor Interface**: Seamlessly switch between different AI agents without changing your core integration logic.
- **Real-Time Log Streaming**: Native support for Server-Sent Events (SSE) allows streaming of real-time execution logs and thought processes back to clients.
- **Session-Based Execution**: Every task runs in an isolated session, making it easy to track, pause, resume, or interrupt.
- **Interactive Continuations**: Supports interactive loops, allowing users to approve tool calls, provide follow-up feedback, or correct executions on the fly.
- **Pluggable Event Store**: Use an in-memory store or build your own persistent event storage (e.g., database) via the SDK.
- **Extensible Architecture**: Easily add new AI CLI tools or remote executors by implementing the core `Executor` interface.

## 🏗️ Architecture

- **API Layer**: Exposes HTTP endpoints and manages SSE connections for real-time observability.
- **Executor Registry**: A centralized registry managing supported agent implementations.
- **Streaming Manager**: Manages SSE subscriptions, buffers logs, and reliably routes events.
- **Transformers**: Standardizes logs emitted from diverse executors (ACP, stream-json, etc.) into a cohesive `.Event` structure.

## 🛠️ Getting Started (Running the API Server)

### Prerequisites

- **Go 1.22+**
- **Node.js & npm** (Required to run `npx`-based executors like Claude Code)
- Requisite CLI tools for your preferred executors installed globally or available via `npx` (e.g., `@anthropic-ai/claude-code`, `@openai/codex`).

### Building and Running

1. **Install dependencies:**
   ```bash
   go mod download
   ```

2. **Build the server:**
   ```bash
   go build -o server ./cmd/server
   ```

3. **Run the server:**
   ```bash
   ./server -addr :8080
   ```
   *(Ensure any required environment variables like API keys for Claude/OpenAI are set before running).*

### HTTP API Endpoints

- `POST /api/execute`: Start a new session.
- `GET /api/execute/{session_id}/stream`: Stream real-time logs via SSE.
- `GET /api/execute/{session_id}/events?after_seq=0&limit=100`: Fetch specific persisted events.
- `POST /api/execute/{session_id}/continue`: Send follow-up prompt/approval.
- `POST /api/execute/{session_id}/interrupt`: Safely stop execution.
- `GET /health`: Health check.

---

## 💻 SDK Quick Start

When embedding the executor within your own Go project, leverage `pkg/sdk`.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/supremeagent/executor/pkg/sdk"
)

func main() {
	ctx := context.Background()
	client := sdk.New()
	defer client.Shutdown()

	// 1) Execute task
	resp, err := client.Execute(ctx, sdk.ExecuteRequest{
		Prompt:     "Scan the current directory and summarize the key files",
		Executor:   sdk.ExecutorCodex, // e.g. sdk.ExecutorClaudeCode, sdk.ExecutorQwen
		WorkingDir: ".",
	})
	if err != nil {
		log.Fatal(err)
	}
	sessionID := resp.SessionID
	fmt.Println("Session:", sessionID)

	// 2) Stream events via channel
	events, cancel := client.Subscribe(sessionID, sdk.SubscribeOptions{
		ReturnAll:    false,
		IncludeDebug: false,
	})
	defer cancel()

	go func() {
		for evt := range events {
			fmt.Printf("[%s] %v\n", evt.Type, evt.Content.Summary)
			if evt.Type == "done" || evt.Type == "error" {
				return
			}
		}
	}()

	// 3) Example: Pause and resume task
	time.Sleep(2 * time.Second)
	if err := client.PauseTask(sessionID); err != nil {
		log.Println("Pause failed:", err)
	}

	time.Sleep(1 * time.Second)
	if err := client.ResumeTask(ctx, sessionID, "Resume execution, providing the final summary"); err != nil {
		log.Println("Resume failed:", err)
	}

	// Keep main alive for streaming
	time.Sleep(10 * time.Second)
}
```

### SDK with Persistent Store and Hooks

You can heavily customize the SDK by utilizing lifecycle hooks and custom event storage:

```go
// Stores events in memory and cleans up completed sessions older than 10 minutes
store := sdk.NewMemoryEventStoreWithExpiration(10 * time.Minute)

client := sdk.NewWithOptions(sdk.ClientOptions{
	EventStore: store,
	Hooks: sdk.Hooks{
		OnSessionStart: func(ctx context.Context, sessionID string, req sdk.ExecuteRequest) {
			log.Printf("Session %s started with executor %s", sessionID, req.Executor)
		},
		OnEventStored: func(ctx context.Context, evt sdk.Event) {
			// Trigger DB saves, webhooks, or log aggregators
		},
		OnStoreError: func(ctx context.Context, sessionID string, evt sdk.Event, err error) {
			log.Printf("Failed to store event for %s: %v", sessionID, err)
		},
	},
})
```
