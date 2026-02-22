package codex

import (
	"encoding/json"
	"testing"
)

func TestRequestIDMarshalUnmarshal(t *testing.T) {
	n := int64(12)
	data, err := json.Marshal(RequestID{Number: &n})
	if err != nil || string(data) != "12" {
		t.Fatalf("marshal number failed: %s err=%v", string(data), err)
	}

	sv := "abc"
	data, err = json.Marshal(RequestID{String: &sv})
	if err != nil || string(data) != `"abc"` {
		t.Fatalf("marshal string failed: %s err=%v", string(data), err)
	}

	var id RequestID
	if err := json.Unmarshal([]byte("null"), &id); err != nil {
		t.Fatalf("unmarshal null failed: %v", err)
	}
	if id.Number != nil || id.String != nil {
		t.Fatalf("expected nil fields for null")
	}

	if err := json.Unmarshal([]byte("123"), &id); err != nil || id.Number == nil || *id.Number != 123 {
		t.Fatalf("unmarshal number failed: id=%+v err=%v", id, err)
	}

	if err := json.Unmarshal([]byte(`"str"`), &id); err != nil || id.String == nil || *id.String != "str" {
		t.Fatalf("unmarshal string failed: id=%+v err=%v", id, err)
	}

	if err := json.Unmarshal([]byte("{}"), &id); err == nil {
		t.Fatalf("expected invalid format error")
	}
}
