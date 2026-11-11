package mcp

import (
	"sync"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/sessionplatform"
)

const (
	// How long session_list keeps reporting a pending create after it ended.
	trackedCreateRetention = 15 * time.Minute
	// A create the control plane never answered is dropped after this; the
	// control plane's own backstop ends a create well before it.
	trackedCreateAbandon = time.Hour
)

// createTracker remembers the session_create calls this server answered
// pending, and how they ended, so session_list can tell a create still
// provisioning from one that failed after the caller left (AC7). It is memory
// only: a restart of this server forgets them.
type createTracker struct {
	mu      sync.Mutex
	entries []*trackedCreate
	now     func() time.Time
}

type trackedCreate struct {
	name         string
	workloadType string
	startedAt    time.Time
	finishedAt   time.Time
	id           string
	err          string
}

func newCreateTracker() *createTracker {
	return &createTracker{now: time.Now}
}

// A nil tracker (a Handler built without NewHandler) tracks nothing.
func (t *createTracker) add(name, workloadType string, startedAt time.Time) *trackedCreate {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := &trackedCreate{name: name, workloadType: workloadType, startedAt: startedAt}
	t.entries = append(t.entries, entry)
	return entry
}

func (t *createTracker) finish(entry *trackedCreate, created *sessionplatform.Session, err error) {
	if t == nil || entry == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entry.finishedAt = t.now()
	if err != nil {
		entry.err = err.Error()
		return
	}
	entry.id = created.ID
}

// report prunes expired entries and returns the rest, oldest first.
func (t *createTracker) report() []any {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	kept := t.entries[:0]
	out := make([]any, 0, len(t.entries))
	for _, e := range t.entries {
		if e.finishedAt.IsZero() && now.Sub(e.startedAt) > trackedCreateAbandon {
			continue
		}
		if !e.finishedAt.IsZero() && now.Sub(e.finishedAt) > trackedCreateRetention {
			continue
		}
		kept = append(kept, e)
		item := map[string]any{
			"name":         e.name,
			"workloadType": e.workloadType,
			"startedAt":    e.startedAt.UTC().Format(time.RFC3339),
		}
		switch {
		case e.finishedAt.IsZero():
			item["state"] = "starting"
		case e.err != "":
			item["state"] = "failed"
			item["error"] = e.err
			item["finishedAt"] = e.finishedAt.UTC().Format(time.RFC3339)
		default:
			item["state"] = "created"
			item["id"] = e.id
			item["finishedAt"] = e.finishedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	t.entries = kept
	return out
}
