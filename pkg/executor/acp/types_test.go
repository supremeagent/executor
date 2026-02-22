package acp

import (
	"encoding/json"
	"testing"
)

func TestParseEvent_SessionStart(t *testing.T) {
	line := `{"SessionStart": "sess-abc-123"}`
	evt, ok := parseEvent([]byte(line))
	if !ok {
		t.Fatal("expected parseEvent to succeed for SessionStart")
	}
	if evt.Type != EventTypeSessionStart {
		t.Errorf("expected EventTypeSessionStart, got %q", evt.Type)
	}
	var id string
	if err := json.Unmarshal(evt.Raw, &id); err != nil {
		t.Fatalf("unmarshal session id: %v", err)
	}
	if id != "sess-abc-123" {
		t.Errorf("expected session id %q, got %q", "sess-abc-123", id)
	}
}

func TestParseEvent_Done(t *testing.T) {
	line := `{"Done": "end_turn"}`
	evt, ok := parseEvent([]byte(line))
	if !ok {
		t.Fatal("expected parseEvent to succeed for Done")
	}
	if evt.Type != EventTypeDone {
		t.Errorf("expected EventTypeDone, got %q", evt.Type)
	}
}

func TestParseEvent_Error(t *testing.T) {
	line := `{"Error": "something went wrong"}`
	evt, ok := parseEvent([]byte(line))
	if !ok {
		t.Fatal("expected parseEvent to succeed for Error")
	}
	if evt.Type != EventTypeError {
		t.Errorf("expected EventTypeError, got %q", evt.Type)
	}
}

func TestParseEvent_Message(t *testing.T) {
	line := `{"Message": {"Text": {"text": "hello"}}}`
	evt, ok := parseEvent([]byte(line))
	if !ok {
		t.Fatal("expected parseEvent to succeed for Message")
	}
	if evt.Type != EventTypeMessage {
		t.Errorf("expected EventTypeMessage, got %q", evt.Type)
	}
}

func TestParseEvent_ToolCall(t *testing.T) {
	line := `{"ToolCall": {"tool_call_id": "read-1", "kind": "Read", "title": "main.go", "status": "pending"}}`
	evt, ok := parseEvent([]byte(line))
	if !ok {
		t.Fatal("expected parseEvent to succeed for ToolCall")
	}
	if evt.Type != EventTypeToolCall {
		t.Errorf("expected EventTypeToolCall, got %q", evt.Type)
	}
}

func TestParseEvent_ApprovalRequest(t *testing.T) {
	line := `{"RequestPermission": {"tool_call_id": "exec-1", "tool_call": {"tool_call_id": "exec-1", "kind": "Execute", "title": "ls", "status": "pending"}}}`
	evt, ok := parseEvent([]byte(line))
	if !ok {
		t.Fatal("expected parseEvent to succeed for RequestPermission")
	}
	if evt.Type != EventTypeApprovalRequest {
		t.Errorf("expected EventTypeApprovalRequest, got %q", evt.Type)
	}
}

func TestParseEvent_UnknownLine(t *testing.T) {
	line := `{"unknownKey": "someValue"}`
	_, ok := parseEvent([]byte(line))
	if ok {
		t.Error("expected parseEvent to fail for unknown event key")
	}
}

func TestParseEvent_InvalidJSON(t *testing.T) {
	_, ok := parseEvent([]byte("not-json"))
	if ok {
		t.Error("expected parseEvent to fail for invalid JSON")
	}
}

func TestParseEvent_EmptyObject(t *testing.T) {
	_, ok := parseEvent([]byte("{}"))
	if ok {
		t.Error("expected parseEvent to fail for empty object with no known type")
	}
}
