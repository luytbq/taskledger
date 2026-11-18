package registry

import (
	"errors"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func add(t *testing.T, db *DB, id string, parent *string) {
	t.Helper()
	if err := db.AddTask(Task{ID: id, Plugin: "p", Locator: id, Registry: Tracked, ParentID: parent, CreatedAt: Stamp(Now())}); err != nil {
		t.Fatal(err)
	}
}

func TestAddTaskRejectsDuplicateID(t *testing.T) {
	db := open(t)
	add(t, db, "A", nil)
	err := db.AddTask(Task{ID: "A", Plugin: "q", Locator: "x", Registry: Tracked, CreatedAt: Stamp(Now())})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("got %v, want ErrDuplicate", err)
	}
}

func TestSetParentRejectsCycle(t *testing.T) {
	db := open(t)
	add(t, db, "A", nil)
	a := "A"
	add(t, db, "B", &a)
	b := "B"
	add(t, db, "C", &b)
	c := "C"
	if err := db.SetParent("A", &c); !errors.Is(err, ErrCycle) {
		t.Fatalf("got %v, want ErrCycle", err)
	}
	if err := db.SetParent("A", &a); !errors.Is(err, ErrCycle) {
		t.Fatalf("self parent: got %v, want ErrCycle", err)
	}
	missing := "Z"
	if err := db.SetParent("A", &missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestForgetDetachesChildrenAndDropsSessions(t *testing.T) {
	db := open(t)
	add(t, db, "A", nil)
	a := "A"
	add(t, db, "B", &a)
	if _, err := db.TouchSession("A", "claude", "s1", "/w", "", Now()); err != nil {
		t.Fatal(err)
	}
	n, err := db.Forget("A")
	if err != nil || n != 1 {
		t.Fatalf("forget: n=%d err=%v", n, err)
	}
	b, err := db.GetTask("B")
	if err != nil || b.ParentID != nil {
		t.Fatalf("child after forget: %+v %v", b, err)
	}
}

func TestTouchSessionKeepsTitleWhenNewTitleEmpty(t *testing.T) {
	db := open(t)
	add(t, db, "A", nil)
	db.TouchSession("A", "claude", "s1", "/w", "first", Now())
	db.TouchSession("A", "claude", "s1", "/w", "", Now())
	s, err := db.Sessions("A", 0)
	if err != nil || len(s) != 1 || s[0].Title != "first" {
		t.Fatalf("sessions: %+v %v", s, err)
	}
}
