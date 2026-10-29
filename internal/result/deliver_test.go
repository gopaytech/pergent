package result

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func shrinkDelays(t *testing.T) {
	t.Helper()
	oldRetry, oldShort, oldDeadline, oldReq := retryDelays, shortRetryDelay, shortDeadline, requestTimeout
	retryDelays = []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}
	shortRetryDelay = time.Millisecond
	shortDeadline = 2 * time.Second
	requestTimeout = 2 * time.Second
	t.Cleanup(func() {
		retryDelays, shortRetryDelay, shortDeadline, requestTimeout = oldRetry, oldShort, oldDeadline, oldReq
	})
}

// statusServer replies with the given statuses in order, repeating the last.
func statusServer(t *testing.T, statuses ...int) (*httptest.Server, *int32) {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&n, 1)) - 1
		if i >= len(statuses) {
			i = len(statuses) - 1
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(statuses[i])
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestDeliver_HTTPSuccess(t *testing.T) {
	shrinkDelays(t)
	var method, ctype, auth string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, ctype, auth = r.Method, r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	data := []byte(`{"status":"ok"}`)
	if err := Deliver(context.Background(), data, Target{URL: srv.URL, Token: "tok"}, io.Discard, false); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %q", method)
	}
	if ctype != "application/json" {
		t.Errorf("content-type = %q", ctype)
	}
	if auth != "Bearer tok" {
		t.Errorf("authorization = %q", auth)
	}
	if !bytes.Equal(body, data) {
		t.Errorf("body = %q", body)
	}
}

func TestDeliver_409IsDelivered(t *testing.T) {
	shrinkDelays(t)
	srv, n := statusServer(t, 503, 409)
	if err := Deliver(context.Background(), []byte("{}"), Target{URL: srv.URL, Token: "tok"}, io.Discard, false); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got := atomic.LoadInt32(n); got != 2 {
		t.Errorf("requests = %d, want 2", got)
	}
}

func TestDeliver_NoRetryOnClientErrors(t *testing.T) {
	for _, code := range []int{400, 401, 403, 413} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			shrinkDelays(t)
			srv, n := statusServer(t, code)
			err := Deliver(context.Background(), []byte("{}"), Target{URL: srv.URL, Token: "tok"}, io.Discard, false)
			if err == nil {
				t.Fatal("want error")
			}
			if got := atomic.LoadInt32(n); got != 1 {
				t.Errorf("requests = %d, want 1", got)
			}
		})
	}
}

func TestDeliver_RetriesThenFails(t *testing.T) {
	shrinkDelays(t)
	srv, n := statusServer(t, 503)
	err := Deliver(context.Background(), []byte("{}"), Target{URL: srv.URL, Token: "tok"}, io.Discard, false)
	if err == nil {
		t.Fatal("want error")
	}
	if got := atomic.LoadInt32(n); got != 4 {
		t.Errorf("requests = %d, want 4", got)
	}
}

func TestDeliver_Retries429(t *testing.T) {
	shrinkDelays(t)
	srv, n := statusServer(t, 429, 200)
	if err := Deliver(context.Background(), []byte("{}"), Target{URL: srv.URL, Token: "tok"}, io.Discard, false); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got := atomic.LoadInt32(n); got != 2 {
		t.Errorf("requests = %d, want 2", got)
	}
}

func TestDeliver_Short(t *testing.T) {
	shrinkDelays(t)
	srv, n := statusServer(t, 503)
	start := time.Now()
	err := Deliver(context.Background(), []byte("{}"), Target{URL: srv.URL, Token: "tok"}, io.Discard, true)
	if err == nil {
		t.Fatal("want error")
	}
	if got := atomic.LoadInt32(n); got != 2 {
		t.Errorf("requests = %d, want 2", got)
	}
	if time.Since(start) > shortDeadline {
		t.Errorf("took %s, over the overall limit", time.Since(start))
	}
}

func TestDeliver_ShortOverallLimit(t *testing.T) {
	shrinkDelays(t)
	shortRetryDelay = 5 * time.Second
	shortDeadline = 100 * time.Millisecond
	srv, n := statusServer(t, 503)
	start := time.Now()
	err := Deliver(context.Background(), []byte("{}"), Target{URL: srv.URL, Token: "tok"}, io.Discard, true)
	if err == nil {
		t.Fatal("want error")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("took %s, the overall limit did not bound the wait", time.Since(start))
	}
	if got := atomic.LoadInt32(n); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}

func TestDeliver_CancelDuringWait(t *testing.T) {
	shrinkDelays(t)
	retryDelays = []time.Duration{5 * time.Second}
	srv, _ := statusServer(t, 503)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Deliver(ctx, []byte("{}"), Target{URL: srv.URL, Token: "tok"}, io.Discard, false)
	if err == nil {
		t.Fatal("want error")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("took %s, cancellation ignored", time.Since(start))
	}
}

func TestDeliver_HungServerTimesOut(t *testing.T) {
	shrinkDelays(t)
	requestTimeout = 50 * time.Millisecond
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	start := time.Now()
	err := Deliver(context.Background(), []byte("{}"), Target{URL: srv.URL, Token: "tok"}, io.Discard, false)
	if err == nil {
		t.Fatal("want error")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %s, request timeout not applied", time.Since(start))
	}
}

func TestDeliver_TokenNeverInError(t *testing.T) {
	shrinkDelays(t)
	const token = "super-secret-token"
	srv, _ := statusServer(t, 401)
	err := Deliver(context.Background(), []byte("{}"), Target{URL: srv.URL, Token: token}, io.Discard, false)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}

	// Connection error: closed server.
	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL
	closed.Close()
	err = Deliver(context.Background(), []byte("{}"), Target{URL: url, Token: token}, io.Discard, false)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}

	// Token embedded in the URL must not leak either.
	err = Deliver(context.Background(), []byte("{}"), Target{URL: url + "/?k=" + token, Token: token}, io.Discard, false)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeliver_FileAndStdout(t *testing.T) {
	shrinkDelays(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "result.json")
	data := []byte(`{"a":1}`)

	if err := Deliver(context.Background(), data, Target{Path: path}, io.Discard, false); err != nil {
		t.Fatalf("file: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file content = %q, %v", got, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp left behind: %v", err)
	}

	var out bytes.Buffer
	if err := Deliver(context.Background(), data, Target{Path: "-"}, &out, false); err != nil {
		t.Fatalf("stdout: %v", err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestDeliver_CombinedErrorNamesEveryTarget(t *testing.T) {
	shrinkDelays(t)
	srv, _ := statusServer(t, 401)
	badPath := filepath.Join(t.TempDir(), "missing-dir", "result.json")
	err := Deliver(context.Background(), []byte("{}"), Target{Path: badPath, URL: srv.URL, Token: "tok"}, io.Discard, false)
	if err == nil {
		t.Fatal("want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "file") || !strings.Contains(msg, "http") {
		t.Errorf("error does not name both targets: %v", err)
	}
}

func TestDeliver_HTTPStillRunsWhenFileFails(t *testing.T) {
	shrinkDelays(t)
	srv, n := statusServer(t, 200)
	badPath := filepath.Join(t.TempDir(), "missing-dir", "result.json")
	_ = Deliver(context.Background(), []byte("{}"), Target{Path: badPath, URL: srv.URL, Token: "tok"}, io.Discard, false)
	if got := atomic.LoadInt32(n); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}

// Captured at package init, before any test shrinks the timing vars.
var (
	initRetryDelays     = append([]time.Duration(nil), retryDelays...)
	initShortRetryDelay = shortRetryDelay
	initShortDeadline   = shortDeadline
)

func TestDeliver_DefaultTimings(t *testing.T) {
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	if len(initRetryDelays) != len(want) {
		t.Fatalf("retryDelays = %v, want %v", initRetryDelays, want)
	}
	for i := range want {
		if initRetryDelays[i] != want[i] {
			t.Fatalf("retryDelays = %v, want %v", initRetryDelays, want)
		}
	}
	if initShortRetryDelay != time.Second {
		t.Errorf("shortRetryDelay = %s, want 1s", initShortRetryDelay)
	}
	if initShortDeadline != 20*time.Second {
		t.Errorf("shortDeadline = %s, want 20s", initShortDeadline)
	}
}

func TestDeliver_RedirectIsNotFollowed(t *testing.T) {
	shrinkDelays(t)
	var targetHits, redirectHits int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&redirectHits, 1)
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	err := Deliver(context.Background(), []byte("{}"), Target{URL: redirector.URL, Token: "tok"}, io.Discard, false)
	if err == nil {
		t.Fatal("want error: a redirect is not a delivery")
	}
	if got := atomic.LoadInt32(&targetHits); got != 0 {
		t.Errorf("redirect target received %d requests, want 0", got)
	}
	if got := atomic.LoadInt32(&redirectHits); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}
