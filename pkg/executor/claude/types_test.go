package claude

import (
	"encoding/json"
	"testing"
)

func TestTypes(t *testing.T) {
	msg := NewUserMessage("hello")
	if msg.Type != "message" || msg.User.Content != "hello" {
		t.Errorf("unexpected message: %v", msg)
	}

	initReq := NewInitializeRequest()
	if initReq.Type != "control_request" || initReq.Request.Subtype != "initialize" {
		t.Errorf("unexpected init request: %v", initReq)
	}

	permReq := NewSetPermissionModeRequest(PermissionModeAcceptEdits)
	if permReq.Request.Mode != PermissionModeAcceptEdits {
		t.Errorf("unexpected perm request: %v", permReq)
	}

	intReq := NewInterruptRequest()
	if intReq.Request.Subtype != "interrupt" {
		t.Errorf("unexpected interrupt request: %v", intReq)
	}

	resp := ControlResponseMessage("req-1", json.RawMessage(`{"status":"ok"}`))
	if resp.Type != "control_response" || resp.Response.Subtype != "success" {
		t.Errorf("unexpected response: %v", resp)
	}

	errResp := ControlErrorResponse("req-2", "something went wrong")
	if errResp.Response.Subtype != "error" || *errResp.Response.Error != "something went wrong" {
		t.Errorf("unexpected error response: %v", errResp)
	}
}
