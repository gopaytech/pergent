package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCall is one tool call the fake model asks opencode to make.
type fakeCall struct {
	Name string
	Args map[string]any
}

// fakeRequest is what the fake model saw in one chat completion request.
type fakeRequest struct {
	Tools       []string
	System      string
	ToolResults map[string]string // by tool-call ID
}

// fakeModel is a scripted OpenAI-compatible server for the real-opencode
// tests. Turn N (N = assistant messages with tool_calls already in the
// request) answers with turns[N] as tool calls; a turn past the end answers
// with an empty findings block. A request without tools (opencode's title
// agent) always gets plain text.
type fakeModel struct {
	URL string

	t *testing.T
	// only, when set, limits the scripted turns to requests whose system
	// prompt contains it: any other run just answers.
	only string
	// next, when it has an entry for turn N, builds that turn's calls from the
	// request that asks for it (so from the results of the earlier turns).
	next map[int]func(fakeRequest) []fakeCall
	mu   sync.Mutex
	reqs []fakeRequest
}

const fakeFinal = "Reviewed.\n\n```json\n{\"summary\":\"ok\",\"confidence\":5,\"confidence_reason\":\"small\",\"findings\":[]}\n```\n"

// newFakeModelFor is newFakeModel with turns served only to the run whose
// system prompt contains only. Other runs of the review get no tool calls.
func newFakeModelFor(t *testing.T, only string, turns [][]fakeCall) *fakeModel {
	t.Helper()
	m := newFakeModel(t, turns, "")
	m.only = only
	return m
}

// newFakeModel starts the server. mode is "", "error500", "overflow" or
// "hang" (never answers until the request's context ends).
func newFakeModel(t *testing.T, turns [][]fakeCall, mode string) *fakeModel {
	t.Helper()
	m := &fakeModel{t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.handle(w, r, turns, mode)
	}))
	t.Cleanup(srv.Close)
	// Runs first: a "hang" request would otherwise block srv.Close.
	t.Cleanup(srv.CloseClientConnections)
	m.URL = srv.URL
	return m
}

// Requests returns a copy of the recorded requests.
func (m *fakeModel) Requests() []fakeRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]fakeRequest(nil), m.reqs...)
}

func fakeContentText(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, p := range v {
			if pm, ok := p.(map[string]any); ok {
				if s, ok := pm["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func (m *fakeModel) handle(w http.ResponseWriter, r *http.Request, turns [][]fakeCall, mode string) {
	if r.Method == http.MethodGet {
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"object":"list","data":[{"id":"fake-model","object":"model"}]}`)
		return
	}
	raw, err := io.ReadAll(r.Body)
	// A request opencode kills mid-flight (a timeout, a stop) arrives cut off:
	// that is not a fault of the fake.
	cutOff := errors.Is(err, context.Canceled) || errors.Is(err, io.ErrUnexpectedEOF) || r.Context().Err() != nil
	if err != nil && !cutOff {
		m.t.Errorf("fake model: reading a request body: %v", err)
	}
	if err != nil && cutOff {
		return
	}
	var req struct {
		Stream   bool             `json:"stream"`
		Tools    []map[string]any `json:"tools"`
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		if len(bytes.TrimSpace(raw)) == 0 && r.Context().Err() != nil {
			return
		}
		m.t.Errorf("fake model: request body is not JSON: %v", err)
	}

	rec := fakeRequest{ToolResults: map[string]string{}}
	var sys []string
	turn := 0
	for _, msg := range req.Messages {
		switch msg["role"] {
		case "system":
			sys = append(sys, fakeContentText(msg["content"]))
		case "tool":
			id, _ := msg["tool_call_id"].(string)
			rec.ToolResults[id] = fakeContentText(msg["content"])
		case "assistant":
			if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
				turn++
			}
		}
	}
	rec.System = strings.Join(sys, "\n")
	for _, tl := range req.Tools {
		if fn, ok := tl["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				rec.Tools = append(rec.Tools, name)
			}
		}
	}
	m.mu.Lock()
	m.reqs = append(m.reqs, rec)
	m.mu.Unlock()

	switch mode {
	case "error500":
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(500)
		io.WriteString(w, `{"error":{"message":"mock internal error","type":"server_error"}}`)
		return
	case "overflow":
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"message":"This model's maximum context length is 1000 tokens. However, your messages resulted in 5000 tokens.","type":"invalid_request_error","code":"context_length_exceeded"}}`)
		return
	case "hang":
		<-r.Context().Done()
		return
	}

	var calls []fakeCall
	if len(req.Tools) > 0 && turn < len(turns) && (m.only == "" || strings.Contains(rec.System, m.only)) {
		calls = turns[turn]
	}
	if fn := m.next[turn]; fn != nil && len(req.Tools) > 0 && (m.only == "" || strings.Contains(rec.System, m.only)) {
		calls = fn(rec)
	}
	base := map[string]any{"id": "c1", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "fake-model"}
	usage := map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}
	chunk := func(delta map[string]any, finish any, withUsage bool) map[string]any {
		c := map[string]any{}
		for k, v := range base {
			c[k] = v
		}
		c["choices"] = []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}
		if withUsage {
			c["usage"] = usage
		}
		return c
	}
	toolCalls := func() []any {
		var out []any
		for i, c := range calls {
			a := c.Args
			if a == nil {
				a = map[string]any{}
			}
			args, _ := json.Marshal(a)
			out = append(out, map[string]any{"id": fmt.Sprintf("call_%d_%d", turn, i), "type": "function",
				"function": map[string]any{"name": c.Name, "arguments": string(args)}})
		}
		return out
	}

	if !req.Stream {
		msg := map[string]any{"role": "assistant", "content": fakeFinal}
		finish := "stop"
		if len(calls) > 0 {
			msg = map[string]any{"role": "assistant", "content": nil, "tool_calls": toolCalls()}
			finish = "tool_calls"
		}
		body := map[string]any{"id": "c1", "object": "chat.completion", "created": time.Now().Unix(), "model": "fake-model",
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}}, "usage": usage}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
		return
	}

	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	send := func(obj any) {
		b, _ := json.Marshal(obj)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	if len(calls) > 0 {
		for i, tc := range toolCalls() {
			one := tc.(map[string]any)
			one["index"] = i
			send(chunk(map[string]any{"role": "assistant", "tool_calls": []any{one}}, nil, false))
		}
		send(chunk(map[string]any{}, "tool_calls", true))
	} else {
		send(chunk(map[string]any{"role": "assistant", "content": fakeFinal}, nil, false))
		send(chunk(map[string]any{}, "stop", true))
	}
	io.WriteString(w, "data: [DONE]\n\n")
}
