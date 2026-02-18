# Gemini Project Context: Executor

## Project Overview
The `executor` project is a Go-based API server that provides a unified interface for executing tasks using AI agents, specifically **Claude Code** and **Codex**. It allows for starting, monitoring, and managing AI-driven code generation and system modification tasks in a controlled environment.

- **Primary Language:** Go 1.22
- **Key Capabilities:**
  - Unified API for multiple AI executors.
  - Real-time output streaming via Server-Sent Events (SSE).
  - Session-based execution management.
  - Support for interactive continuations and interruptions.

## Architecture

### Components
- **API Layer (`pkg/api`):** Handles HTTP requests, manages SSE connections, and defines the RESTful interface.
- **Executor Registry (`pkg/executor`):** A centralized registry that manages different executor implementations (Claude, Codex).
- **Claude Executor (`pkg/executor/claude`):** Wraps the Claude Code CLI (`@anthropic-ai/claude-code`) using `npx`.
- **Codex Executor (`pkg/executor/codex`):** Wraps the Codex app-server (`@openai/codex`) using JSON-RPC over stdin/stdout.
- **Streaming Manager (`pkg/streaming`):** Manages SSE subscriptions and buffers log entries for active sessions.

### API Endpoints
- `POST /api/execute`: Start a new execution session.
- `GET /api/execute/{session_id}/stream`: Stream real-time logs via SSE.
- `POST /api/execute/{session_id}/continue`: Send follow-up messages to an active session.
- `POST /api/execute/{session_id}/interrupt`: Stop a running execution.
- `GET /health`: Basic health check.

## Building and Running

### Prerequisites
- Go 1.22+
- Node.js & npm (to run executors via `npx`)
- Claude Code and/or Codex CLI tools installed/available via `npx`.

### Commands
- **Install Dependencies:** `go mod download`
- **Build Server:** `go build -o server ./cmd/server`
- **Run Server:** `./server -addr :8080`
- **Run Tests:** `go test ./...`
- **Run Example Test Script:** `./test-api.sh "Create a simple hello.go file" /tmp/test-dir`

## Development Conventions

- **Executor Interface:** All new AI executors must implement the `Executor` interface defined in `pkg/executor/executor.go`.
- **Log Streaming:** Use the `Logs()` channel on the `Executor` interface to stream output back to the API layer.
- **Error Handling:** Use custom error types defined in `pkg/executor/errors.go` where appropriate.
- **Concurrency:** The server heavily uses goroutines for background task execution and SSE log piping. Ensure proper synchronization when modifying shared state in the `Registry` or `Manager`.

## Key Files
- `cmd/server/main.go`: Application entry point.
- `pkg/api/routes.go`: API route definitions.
- `pkg/api/handlers.go`: API request logic.
- `pkg/executor/executor.go`: Core interfaces and Registry implementation.
- `pkg/executor/claude/client.go`: Claude Code integration logic.
- `pkg/executor/codex/client.go`: Codex integration logic.
- `test-api.sh`: A comprehensive bash script for testing the API end-to-end.
