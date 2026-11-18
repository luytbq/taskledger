// Package plugin runs taskledger plugins and checks what they return against the
// protocol schema. A plugin is any executable: core runs "<command> <verb>",
// writes one JSON request to stdin, and reads one JSON object from stdout.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"taskledger/internal/config"
	"taskledger/internal/jsonx"
)

// Protocol versions this core accepts from plugins.
const (
	ProtocolMin = 1
	ProtocolMax = 1
)

var States = []string{"needs-me", "active", "waiting", "paused", "closed"}

func StateRank(s string) int {
	if i := slices.Index(States, s); i >= 0 {
		return i
	}
	return 0
}

type TaskRef struct {
	ID      string `json:"id"`
	Locator string `json:"locator"`
}

type Request struct {
	Protocol int      `json:"protocol"`
	Verb     string   `json:"verb"`
	Task     *TaskRef `json:"task"`
	Args     []string `json:"args"`
	Input    any      `json:"input"`
}

type NewInput struct {
	ID     *string  `json:"id"`
	Title  string   `json:"title"`
	Parent *TaskRef `json:"parent"`
}

type LocateInput struct {
	ID string `json:"id"`
}

type GuideInput struct {
	Topic  *string `json:"topic"`
	Status Status  `json:"status"`
}

type Hello struct {
	Protocol int      `json:"protocol"`
	Verbs    []string `json:"verbs"`
	Children bool     `json:"children"`
}

func (h Hello) Supports(verb string) bool { return slices.Contains(h.Verbs, verb) }

type NewResult struct {
	ID      string `json:"id"`
	Locator string `json:"locator"`
	Title   string `json:"title"`
}

type Status struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	State        string   `json:"state"`
	Stage        string   `json:"stage"`
	LastActivity *string  `json:"last_activity"`
	Next         string   `json:"next"`
	Due          *string  `json:"due"`
	Flags        []string `json:"flags"`
	Tags         []string `json:"tags"`
}

var statusKeys = []string{"id", "title", "state", "stage", "last_activity", "next", "due", "flags", "tags"}

type HistoryEntry struct {
	Time  string  `json:"time"`
	Stage *string `json:"stage"`
	Event string  `json:"event"`
	Note  string  `json:"note"`
}

type Show struct {
	Status
	Summary string         `json:"summary"`
	Pending []string       `json:"pending"`
	Recent  []HistoryEntry `json:"recent"`
}

type LogResult struct {
	OK     bool          `json:"ok"`
	Reason *string       `json:"reason"`
	Entry  *HistoryEntry `json:"entry"`
}

type GuideSection struct {
	Title string   `json:"title"`
	Items []string `json:"items"`
}

type GuideRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	When string `json:"when"`
}

type GuideTopic struct {
	Name string `json:"name"`
	When string `json:"when"`
}

type Guide struct {
	Topic    *string        `json:"topic"`
	Rules    []string       `json:"rules"`
	Sections []GuideSection `json:"sections"`
	Refs     []GuideRef     `json:"refs"`
	Topics   []GuideTopic   `json:"topics"`
}

type LocateResult struct {
	Found   bool    `json:"found"`
	Locator *string `json:"locator"`
	Title   *string `json:"title"`
}

type PullResult struct {
	OK      bool     `json:"ok"`
	Error   *string  `json:"error"`
	Changed []string `json:"changed"`
}

// Error kinds the CLI maps to error codes.
var (
	ErrTimeout  = errors.New("plugin timed out")
	ErrNotFound = errors.New("plugin command not found")
)

// CallError is a plugin failure: non-zero exit, bad JSON, or a schema violation.
type CallError struct {
	Verb   string
	Reason string
}

func (e *CallError) Error() string { return fmt.Sprintf("plugin %s: %s", e.Verb, e.Reason) }

type Client struct {
	Name    string
	Command string
	Env     []string
	Timeout time.Duration
}

func NewClient(name string, p config.Plugin) (*Client, error) {
	d, err := p.TimeoutDuration()
	if err != nil {
		return nil, err
	}
	return &Client{Name: name, Command: config.Expand(p.Command), Env: p.ExpandedEnv(), Timeout: d}, nil
}

// Resolve returns the executable path and its modification time, which keys the
// hello cache.
func (c *Client) Resolve() (string, int64, error) {
	path, err := exec.LookPath(c.Command)
	if err != nil {
		return "", 0, ErrNotFound
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", 0, ErrNotFound
	}
	return path, st.ModTime().UnixNano(), nil
}

func (c *Client) command(ctx context.Context, verb string, req Request) (*exec.Cmd, error) {
	path, _, err := c.Resolve()
	if err != nil {
		return nil, err
	}
	if req.Args == nil {
		req.Args = []string{}
	}
	req.Protocol = ProtocolMax
	req.Verb = verb
	body, err := jsonx.Marshal(req)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, verb)
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Stdin = bytes.NewReader(body)
	return cmd, nil
}

// Call runs one JSON verb and decodes the reply into out after checking that every
// key in required is present.
func (c *Client) Call(verb string, req Request, out any, required ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	cmd, err := c.command(ctx, verb, req)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, ErrTimeout
	}
	if err != nil {
		reason := lastLine(stderr.String())
		if reason == "" {
			reason = err.Error()
		}
		return nil, &CallError{Verb: verb, Reason: reason}
	}
	raw := stdout.Bytes()
	missing, err := jsonx.RequireKeys(raw, required...)
	if err != nil {
		return nil, &CallError{Verb: verb, Reason: "reply is not a JSON object: " + err.Error()}
	}
	if missing != "" {
		return nil, &CallError{Verb: verb, Reason: fmt.Sprintf("reply has no %q field", missing)}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, &CallError{Verb: verb, Reason: "reply does not match the schema: " + err.Error()}
	}
	return raw, nil
}

// Run executes a pass-through verb. The plugin talks straight to the terminal and
// its exit code goes back to the caller unchanged.
func (c *Client) Run(task TaskRef, args []string) (int, error) {
	cmd, err := c.command(context.Background(), "run", Request{Task: &task, Args: args})
	if err != nil {
		return 0, err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}

func (c *Client) Hello() (Hello, []byte, error) {
	var h Hello
	raw, err := c.Call("hello", Request{}, &h, "protocol", "verbs", "children")
	if err != nil {
		return h, nil, err
	}
	if !slices.Contains(h.Verbs, "hello") {
		return h, nil, &CallError{Verb: "hello", Reason: "verbs must include hello"}
	}
	return h, raw, nil
}

func (h Hello) ProtocolOK() bool { return h.Protocol >= ProtocolMin && h.Protocol <= ProtocolMax }

func ref(t TaskRef) *TaskRef { return &t }

func (c *Client) Status(t TaskRef) (Status, error) {
	var s Status
	if _, err := c.Call("status", Request{Task: ref(t)}, &s, statusKeys...); err != nil {
		return s, err
	}
	return s, CheckStatus("status", &s)
}

// CheckStatus enforces the parts of Status that JSON decoding cannot.
func CheckStatus(verb string, s *Status) error {
	if !slices.Contains(States, s.State) {
		return &CallError{Verb: verb, Reason: fmt.Sprintf("state %q is not one of %s", s.State, strings.Join(States, ", "))}
	}
	if s.LastActivity != nil {
		if _, err := time.Parse(time.RFC3339, *s.LastActivity); err != nil {
			return &CallError{Verb: verb, Reason: fmt.Sprintf("last_activity %q is not RFC 3339", *s.LastActivity)}
		}
	}
	if s.Due != nil {
		if _, err := time.Parse("2006-01-02", *s.Due); err != nil {
			return &CallError{Verb: verb, Reason: fmt.Sprintf("due %q is not YYYY-MM-DD", *s.Due)}
		}
	}
	if s.Flags == nil {
		s.Flags = []string{}
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return nil
}

func (c *Client) Show(t TaskRef) (Show, error) {
	var s Show
	keys := append(slices.Clone(statusKeys), "summary", "pending", "recent")
	if _, err := c.Call("show", Request{Task: ref(t)}, &s, keys...); err != nil {
		return s, err
	}
	if err := CheckStatus("show", &s.Status); err != nil {
		return s, err
	}
	if s.Pending == nil {
		s.Pending = []string{}
	}
	if s.Recent == nil {
		s.Recent = []HistoryEntry{}
	}
	return s, nil
}

func (c *Client) New(in NewInput, args []string) (NewResult, error) {
	var r NewResult
	_, err := c.Call("new", Request{Args: args, Input: in}, &r, "id", "locator", "title")
	return r, err
}

func (c *Client) Log(t TaskRef, args []string) (LogResult, error) {
	var r LogResult
	_, err := c.Call("log", Request{Task: ref(t), Args: args}, &r, "ok", "reason", "entry")
	if err == nil && r.OK && r.Entry == nil {
		err = &CallError{Verb: "log", Reason: "ok is true but entry is null"}
	}
	return r, err
}

func (c *Client) Guide(t TaskRef, in GuideInput, args []string) (Guide, error) {
	var g Guide
	_, err := c.Call("guide", Request{Task: ref(t), Args: args, Input: in}, &g, "topic", "rules", "sections", "refs", "topics")
	return g.Normalize(), err
}

func (g Guide) Normalize() Guide {
	if g.Rules == nil {
		g.Rules = []string{}
	}
	if g.Sections == nil {
		g.Sections = []GuideSection{}
	}
	if g.Refs == nil {
		g.Refs = []GuideRef{}
	}
	if g.Topics == nil {
		g.Topics = []GuideTopic{}
	}
	return g
}

func (c *Client) Locate(id string) (LocateResult, error) {
	var r LocateResult
	_, err := c.Call("locate", Request{Input: LocateInput{ID: id}}, &r, "found", "locator", "title")
	if err == nil && r.Found && r.Locator == nil {
		err = &CallError{Verb: "locate", Reason: "found is true but locator is null"}
	}
	return r, err
}

func (c *Client) Pull(t TaskRef) (PullResult, error) {
	var r PullResult
	_, err := c.Call("pull", Request{Task: ref(t)}, &r, "ok", "error", "changed")
	return r, err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}
