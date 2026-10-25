package toolscope

import (
	"context"
	"testing"
)

func TestUnscopedAllowsEverything(t *testing.T) {
	ctx := context.Background()
	if Restricted(ctx) {
		t.Error("a bare context must not be restricted")
	}
	if !Allows(ctx, "resource_delete") {
		t.Error("a bare context must allow every tool")
	}
}

func TestScopedAllowsOnlyItsTools(t *testing.T) {
	ctx := With(context.Background(), []string{"ping", "opensearch_search"})
	if !Restricted(ctx) {
		t.Fatal("With must restrict")
	}
	for tool, want := range map[string]bool{"ping": true, "opensearch_search": true, "resource_delete": false, "": false} {
		if got := Allows(ctx, tool); got != want {
			t.Errorf("Allows(%q) = %v, want %v", tool, got, want)
		}
	}
}

func TestEmptyScopeAllowsNothing(t *testing.T) {
	ctx := With(context.Background(), nil)
	if !Restricted(ctx) || Allows(ctx, "ping") {
		t.Error("an empty scope must be restricted and allow nothing")
	}
}
