package board

import (
	"testing"
	"time"

	"taskledger/internal/plugin"
	"taskledger/internal/registry"
)

func item(id string, parent, state string) Item {
	t := registry.Task{ID: id, Plugin: "p", Registry: registry.Tracked}
	if parent != "" {
		t.ParentID = &parent
	}
	if state == "" {
		return Item{Task: t}
	}
	return Item{Task: t, Status: &plugin.Status{ID: id, State: state, Flags: []string{}, Tags: []string{}}}
}

func group(b *Board, state string) *Group {
	for _, g := range b.Groups {
		if g.State == state {
			return g
		}
	}
	return nil
}

func TestTreeSitsInMostUrgentGroupAndMarksMatchingNodes(t *testing.T) {
	b := Build([]Item{item("P", "", "waiting"), item("C", "P", "needs-me")}, nil, time.Now())
	g := group(b, "needs-me")
	if g == nil || len(g.Trees) != 1 || g.Trees[0].ID != "P" {
		t.Fatalf("tree not in needs-me: %+v", b.Groups)
	}
	if g.Trees[0].Marked || !g.Trees[0].Children[0].Marked {
		t.Fatalf("marks wrong: parent=%v child=%v", g.Trees[0].Marked, g.Trees[0].Children[0].Marked)
	}
}

func TestFailedPluginCountsAsNeedsMe(t *testing.T) {
	b := Build([]Item{item("X", "", "")}, nil, time.Now())
	if g := group(b, "needs-me"); g == nil || len(g.Trees) != 1 || !g.Trees[0].Marked {
		t.Fatalf("got %+v", b.Groups)
	}
}

func TestAllChildrenClosedFlag(t *testing.T) {
	b := Build([]Item{item("P", "", "active"), item("C", "P", "closed")}, nil, time.Now())
	p := group(b, "active").Trees[0]
	if !contains(p.Flags, "all children closed") {
		t.Fatalf("flags: %v", p.Flags)
	}
}

func TestChildOfArchivedParentIsOwnRoot(t *testing.T) {
	b := Build([]Item{item("C", "P", "active")}, map[string]bool{"P": true}, time.Now())
	c := group(b, "active").Trees[0]
	if c.ID != "C" || !contains(c.Flags, "parent archived") {
		t.Fatalf("got %+v", c)
	}
}

func TestOverdueFlag(t *testing.T) {
	it := item("A", "", "active")
	due := "2000-01-01"
	it.Status.Due = &due
	b := Build([]Item{it}, nil, time.Now())
	if !contains(group(b, "active").Trees[0].Flags, "overdue") {
		t.Fatal("missing overdue")
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
