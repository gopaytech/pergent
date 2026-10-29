package result

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Retry timing for HTTP delivery. Package vars so tests can shrink them.
var (
	// retryDelays are the waits before each retry in normal mode.
	retryDelays = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	// shortRetryDelay is the single retry wait in short mode (after SIGTERM).
	shortRetryDelay = time.Second
	// shortDeadline bounds the whole HTTP delivery in short mode.
	shortDeadline = 20 * time.Second
	// requestTimeout bounds one HTTP request so a hung server cannot block.
	requestTimeout = 30 * time.Second
)

// Target says where to send the encoded result. Path is a file, or "-" for
// stdout. URL is empty when HTTP delivery is unused.
type Target struct {
	Path  string
	URL   string
	Token string
}

// Deliver sends data to every configured target and returns a combined error
// naming each target that failed. A failing target does not stop the others.
// With short set (after SIGTERM) HTTP uses one retry and a 20s overall limit.
func Deliver(ctx context.Context, data []byte, t Target, stdout io.Writer, short bool) error {
	var errs []error
	if t.Path == "-" {
		if _, err := stdout.Write(data); err != nil {
			errs = append(errs, fmt.Errorf("stdout: %w", err))
		}
	} else if t.Path != "" {
		if err := writeFile(t.Path, data); err != nil {
			errs = append(errs, fmt.Errorf("file: %w", err))
		}
	}
	if t.URL != "" {
		if err := postResult(ctx, data, t, short); err != nil {
			errs = append(errs, fmt.Errorf("http: %w", err))
		}
	}
	return errors.Join(errs...)
}

// writeFile writes to <path>.tmp and renames it over path, so a reader never
// sees a partial file.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func postResult(ctx context.Context, data []byte, t Target, short bool) error {
	delays := retryDelays
	if short {
		delays = []time.Duration{shortRetryDelay}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, shortDeadline)
		defer cancel()
	}

	var last error
	for attempt := 0; ; attempt++ {
		retry, err := postOnce(ctx, data, t)
		if err == nil {
			return nil
		}
		last = err
		if !retry || attempt >= len(delays) {
			return last
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w (gave up retrying: %s)", last, ctx.Err())
		case <-timer.C:
		}
	}
}

// postOnce makes one request. retry reports whether a failure is transient.
func postOnce(ctx context.Context, data []byte, t Target) (retry bool, err error) {
	rctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodPost, t.URL, bytes.NewReader(data))
	if err != nil {
		return false, errors.New("invalid result URL")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return true, fmt.Errorf("request failed: %s", scrub(err, t.Token))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	switch code := resp.StatusCode; {
	case code >= 200 && code < 300:
		return false, nil
	case code == http.StatusConflict:
		// The endpoint already has this result; that is delivered.
		return false, nil
	case code == http.StatusTooManyRequests || code >= 500:
		return true, fmt.Errorf("server replied %d", code)
	default:
		return false, fmt.Errorf("server replied %d", code)
	}
}

// scrub returns the error text without the request URL and without the token.
func scrub(err error, token string) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	msg := err.Error()
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "[redacted]")
	}
	return msg
}
