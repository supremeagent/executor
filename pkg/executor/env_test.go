package executor

import (
	"strings"
	"testing"
)

func TestBuildCommandEnv(t *testing.T) {
	env := BuildCommandEnv(
		map[string]string{"FOO": "one"},
		map[string]string{"FOO": "two", "BAR": "ok", "": "ignored"},
	)

	foundFoo := false
	foundBar := false
	foundEmpty := false
	for _, kv := range env {
		if kv == "FOO=two" {
			foundFoo = true
		}
		if kv == "BAR=ok" {
			foundBar = true
		}
		if strings.HasPrefix(kv, "=") {
			foundEmpty = true
		}
	}

	if !foundFoo {
		t.Fatal("expected FOO override to be applied")
	}
	if !foundBar {
		t.Fatal("expected BAR to be present")
	}
	if foundEmpty {
		t.Fatal("expected empty key to be ignored")
	}
}
