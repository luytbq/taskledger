// Package board builds the parent/child task trees, places each tree in the most
// urgent group found inside it, and renders the text table.
package board

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"taskledger/internal/plugin"
	"taskledger/internal/registry"
)

// Item is one tracked task with whatever core learned about it this run.
// Status is nil when the plugin could not be asked; Flags then explain why.
type Item struct {
	Task   registry.Task
	Status *plugin.Status
	Flags  []string
}

type Node struct {
	ID           string  `json:"id"`
	Plugin       string  `json:"plugin"`
	Title        string  `json:"title"`
	State        *string `json:"state"`
	Stage        string  `json:"stage"`
	LastActivity *string `json:"last_activity"`
	IdleDays     *int    `json:"idle_days"`
	Next         string  `json:"next"`
	Due          *string `json:"due"`
	Marked       bool    `json:"marked"`
	Flags        []string `json:"flags"`
	Tags         []string `json:"tags"`
	Children     []*Node  `json:"children"`

	lastAt *time.Time
}

type Group struct {
	State string  `json:"state"`
	Trees []*Node `json:"trees"`
}

type Board struct {
	GeneratedAt string   `json:"generated_at"`
	Groups      []*Group `json:"groups"`
}

// effective is the state used for grouping: a task core could not read needs the
// user, so it sorts with needs-me.
func (n *Node) effective() string {
	if n.State == nil {
		return "needs-me"
	}
	return *n.State
}

// Build arranges the tracked items into trees. archived holds the ids of archived
// tasks, so a tracked child of an archived parent can say so.
func Build(items []Item, archived map[string]bool, now time.Time) *Board {
	nodes := map[string]*Node{}
	for _, it := range items {
		nodes[it.Task.ID] = newNode(it, now)
	}
	var roots []*Node
	for _, it := range items {
		n := nodes[it.Task.ID]
		if p := it.Task.ParentID; p != nil {
			if parent, ok := nodes[*p]; ok {
				parent.Children = append(parent.Children, n)
				continue
			}
			if archived[*p] {
				n.Flags = append(n.Flags, "parent archived")
			}
		}
		roots = append(roots, n)
	}
	for _, r := range roots {
		finish(r)
	}

	b := &Board{GeneratedAt: registry.Stamp(now), Groups: []*Group{}}
	byState := map[string]*Group{}
	for _, r := range roots {
		st := treeState(r)
		g := byState[st]
		if g == nil {
			g = &Group{State: st}
			byState[st] = g
		}
		mark(r, st)
		g.Trees = append(g.Trees, r)
	}
	for _, st := range plugin.States {
		g := byState[st]
		if g == nil {
			continue
		}
		sort.SliceStable(g.Trees, func(i, j int) bool {
			return before(oldestMarked(g.Trees[i]), oldestMarked(g.Trees[j]), g.Trees[i].ID, g.Trees[j].ID)
		})
		b.Groups = append(b.Groups, g)
	}
	return b
}

func newNode(it Item, now time.Time) *Node {
	n := &Node{ID: it.Task.ID, Plugin: it.Task.Plugin, Title: it.Task.Title, Flags: []string{}, Tags: []string{}, Children: []*Node{}}
	if s := it.Status; s != nil {
		state := s.State
		n.State = &state
		n.Title = s.Title
		n.Stage = s.Stage
		n.Next = s.Next
		n.Due = s.Due
		n.LastActivity = s.LastActivity
		n.Flags = append(n.Flags, s.Flags...)
		n.Tags = append(n.Tags, s.Tags...)
		if s.LastActivity != nil {
			if t, err := time.Parse(time.RFC3339, *s.LastActivity); err == nil {
				n.lastAt = &t
				days := dayDiff(t, now)
				n.IdleDays = &days
			}
		}
		if s.Due != nil && state != "closed" {
			if d, err := time.ParseInLocation("2006-01-02", *s.Due, registry.VN); err == nil && d.Before(startOfDay(now)) {
				n.Flags = append(n.Flags, "overdue")
			}
		}
	}
	n.Flags = append(n.Flags, it.Flags...)
	return n
}

// finish sorts children and adds the flags that depend on them.
func finish(n *Node) {
	for _, c := range n.Children {
		finish(c)
	}
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		ra, rb := plugin.StateRank(a.effective()), plugin.StateRank(b.effective())
		if ra != rb {
			return ra < rb
		}
		return before(a.lastAt, b.lastAt, a.ID, b.ID)
	})
	if len(n.Children) > 0 && n.effective() != "closed" {
		all := true
		for _, c := range n.Children {
			if c.effective() != "closed" {
				all = false
				break
			}
		}
		if all {
			n.Flags = append(n.Flags, "all children closed")
		}
	}
}

func treeState(n *Node) string {
	best := n.effective()
	for _, c := range n.Children {
		if s := treeState(c); plugin.StateRank(s) < plugin.StateRank(best) {
			best = s
		}
	}
	return best
}

func mark(n *Node, group string) {
	n.Marked = n.effective() == group
	for _, c := range n.Children {
		mark(c, group)
	}
}

// oldestMarked is the oldest activity among the nodes that put the tree in its
// group: those are the lines the user has to act on.
func oldestMarked(n *Node) *time.Time {
	var best *time.Time
	var walk func(*Node)
	walk = func(x *Node) {
		if x.Marked {
			// No activity at all counts as the oldest possible.
			t := time.Time{}
			if x.lastAt != nil {
				t = *x.lastAt
			}
			if best == nil || t.Before(*best) {
				best = &t
			}
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return best
}

// before orders older activity first; a task with no activity is the oldest.
func before(a, b *time.Time, idA, idB string) bool {
	switch {
	case a == nil && b == nil:
		return idA < idB
	case a == nil:
		return true
	case b == nil:
		return false
	case !a.Equal(*b):
		return a.Before(*b)
	}
	return idA < idB
}

func startOfDay(t time.Time) time.Time {
	t = t.In(registry.VN)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, registry.VN)
}

func dayDiff(t, now time.Time) int {
	return int(startOfDay(now).Sub(startOfDay(t)).Hours() / 24)
}

var groupTitles = map[string]string{
	"needs-me": "NEEDS ME",
	"active":   "ACTIVE",
	"waiting":  "WAITING",
	"paused":   "PAUSED",
	"closed":   "CLOSED",
}

const titleWidth = 22

type row struct {
	marker, id, plugin, title, stage, when, idle, rest string
}

// Render writes the text board. Columns are padded by display width, so
// Vietnamese titles line up.
func Render(w io.Writer, b *Board, total int) {
	fmt.Fprintf(w, "%d tasks tracked\n", total)
	var rows [][]row
	widths := make([]int, 6)
	for _, g := range b.Groups {
		var rs []row
		for _, t := range g.Trees {
			collect(t, "", "", &rs)
		}
		for _, r := range rs {
			for i, s := range []string{r.id, r.plugin, r.title, r.stage, r.when, r.idle} {
				widths[i] = max(widths[i], runewidth.StringWidth(s))
			}
		}
		rows = append(rows, rs)
	}
	for gi, g := range b.Groups {
		fmt.Fprintf(w, "\n%s\n", groupTitles[g.State])
		for _, r := range rows[gi] {
			cols := []string{r.id, r.plugin, r.title, r.stage, r.when, r.idle}
			var sb strings.Builder
			sb.WriteString(r.marker)
			for i, c := range cols {
				sb.WriteString(runewidth.FillRight(c, widths[i]))
				sb.WriteString("  ")
			}
			line := strings.TrimRight(sb.String(), " ")
			if r.rest != "" {
				line += "  " + r.rest
			}
			fmt.Fprintln(w, line)
		}
	}
}

func collect(n *Node, prefix, childPrefix string, out *[]row) {
	marker := "  "
	if n.Marked {
		marker = "* "
	}
	r := row{
		marker: marker,
		id:     prefix + n.ID,
		plugin: n.Plugin,
		title:  runewidth.Truncate(n.Title, titleWidth, "…"),
		stage:  n.Stage,
	}
	if n.lastAt != nil {
		r.when = n.lastAt.In(registry.VN).Format("02/01 15:04")
	}
	if n.IdleDays != nil {
		if *n.IdleDays == 0 {
			r.idle = "today"
		} else {
			r.idle = fmt.Sprintf("%dd", *n.IdleDays)
		}
	}
	var rest []string
	if n.Next != "" {
		rest = append(rest, "--> "+n.Next)
	}
	var tail []string
	if n.Due != nil {
		tail = append(tail, "due "+*n.Due)
	}
	tail = append(tail, n.Flags...)
	tail = append(tail, n.Tags...)
	if len(tail) > 0 {
		rest = append(rest, "["+strings.Join(tail, "; ")+"]")
	}
	r.rest = strings.Join(rest, "  ")
	*out = append(*out, r)
	for i, c := range n.Children {
		branch, cont := "├─ ", "│  "
		if i == len(n.Children)-1 {
			branch, cont = "└─ ", "   "
		}
		collect(c, childPrefix+branch, childPrefix+cont, out)
	}
}
