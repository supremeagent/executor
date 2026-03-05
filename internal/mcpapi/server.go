package mcpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// jsonrpc message types
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool is an MCP tool definition with JSON Schema input.
type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema jsonSchema `json:"inputSchema"`
}

type jsonSchema struct {
	Type        string                `json:"type"`
	Properties  map[string]jsonSchema `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
	Description string                `json:"description,omitempty"`
	Enum        []string              `json:"enum,omitempty"`
}

// ToolResult is the MCP tool call result.
type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem is a single piece of content in a ToolResult.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolHandler processes a tool call and returns a result.
type ToolHandler func(ctx context.Context, args map[string]any) (ToolResult, error)

// Server is an MCP server that communicates over stdio using JSON-RPC 2.0.
type Server struct {
	mu       sync.Mutex
	tools    map[string]ToolHandler
	toolList []Tool
	in       io.Reader
	out      io.Writer
}

// NewServer creates a new MCP server reading from in and writing to out.
func NewServer(in io.Reader, out io.Writer) *Server {
	return &Server{
		tools: make(map[string]ToolHandler),
		in:    in,
		out:   out,
	}
}

// RegisterTool registers a tool definition and its handler.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.tools[tool.Name] = handler
	s.toolList = append(s.toolList, tool)
}

// Serve reads JSON-RPC requests line-by-line and dispatches them until ctx is done or EOF.
func (s *Server) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "parse error")
			continue
		}

		if resp := s.dispatch(ctx, req); resp != nil {
			s.send(*resp)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	return scanner.Err()
}

// HTTPHandler returns an http.Handler that serves MCP over HTTP (Streamable HTTP transport).
// Clients POST JSON-RPC messages to the endpoint and receive JSON-RPC responses.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(response{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			return
		}

		resp := s.dispatch(r.Context(), req)
		if resp == nil {
			// notification – no content
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// dispatch processes one JSON-RPC request and returns the response, or nil for notifications.
func (s *Server) dispatch(ctx context.Context, req request) *response {
	result := func(v any) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Result: v}
	}
	rpcErr := func(code int, msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}

	switch req.Method {
	case "initialize":
		return result(map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "executor", "version": "1.0.0"},
		})

	case "initialized", "notifications/initialized":
		return nil // notification – no response

	case "ping":
		return result(map[string]any{})

	case "tools/list":
		return result(map[string]any{"tools": s.toolList})

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return rpcErr(-32602, "invalid params")
		}
		handler, ok := s.tools[params.Name]
		if !ok {
			return rpcErr(-32601, fmt.Sprintf("tool not found: %s", params.Name))
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}
		toolResult, err := handler(ctx, params.Arguments)
		if err != nil {
			return result(ToolResult{
				Content: []ContentItem{{Type: "text", Text: err.Error()}},
				IsError: true,
			})
		}
		return result(toolResult)

	default:
		if req.ID != nil {
			return rpcErr(-32601, fmt.Sprintf("method not found: %s", req.Method))
		}
		return nil
	}
}

func (s *Server) sendError(id any, code int, message string) {
	s.send(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

func (s *Server) send(resp response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(resp)
	_, _ = fmt.Fprintf(s.out, "%s\n", data)
}

// TextResult returns a successful ToolResult with a JSON-encoded value.
func TextResult(v any) ToolResult {
	text, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: string(text)}}}
}

// ErrorResult returns a ToolResult representing an error.
func ErrorResult(err error) ToolResult {
	return ToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
}
