package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

const testToken = "s3cr3t-sender-token"

func newTestClient(url string) *Client {
	c := New(url, testToken)
	c.Backoff = func(int) time.Duration { return 0 }
	return c
}

func TestClaimRequestAndResponse(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/outbox/claim" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("Authorization: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type: %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		io.WriteString(w, `{"items":[{"key":"1|https://codeforces.com/contests/2000|2026-11-07T14:35:00Z","text":"Codeforces Round\nstarts soon","startUtc":"2026-11-07T14:35:00Z","deadlineUtc":"2026-11-07T14:25:00Z"}]}`)
	}))
	defer srv.Close()

	items, err := newTestClient(srv.URL+"/").Claim(context.Background(), "claim-1")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if len(gotBody) != 1 || gotBody["claimId"] != "claim-1" {
		t.Errorf("claim body: %v", gotBody)
	}
	want := Item{
		Key:         "1|https://codeforces.com/contests/2000|2026-11-07T14:35:00Z",
		Text:        "Codeforces Round\nstarts soon",
		StartUTC:    "2026-11-07T14:35:00Z",
		DeadlineUTC: "2026-11-07T14:25:00Z",
	}
	if len(items) != 1 || items[0] != want {
		t.Errorf("items: got %+v, want [%+v]", items, want)
	}
}

func TestClaimEmpty(t *testing.T) {
	for _, body := range []string{`{"items":[]}`, `{}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, body)
		}))
		items, err := newTestClient(srv.URL).Claim(context.Background(), "c")
		srv.Close()
		if err != nil || len(items) != 0 {
			t.Errorf("body %s: got %v, %v", body, items, err)
		}
	}
}

func TestClaimIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Claim(context.Background(), "c")
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusInternalServerError {
		t.Fatalf("Claim error: got %v, want StatusError 500", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error leaks token: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("claim requests: got %d, want 1", n)
	}
}

func TestClaimRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"items":`)
	}))
	defer srv.Close()
	if _, err := newTestClient(srv.URL).Claim(context.Background(), "c"); err == nil {
		t.Fatal("Claim accepted malformed JSON")
	}
}

func TestErrorBodyIsTruncatedToValidUTF8(t *testing.T) {
	body := "x" + strings.Repeat("é", maxErrorBody)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, body, http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Claim(context.Background(), "c")
	se, ok := errors.AsType[*StatusError](err)
	if !ok {
		t.Fatalf("Claim: got %v, want *StatusError", err)
	}
	if !utf8.ValidString(se.Body) {
		t.Errorf("body is not valid UTF-8: %q", se.Body)
	}
	if !strings.HasSuffix(se.Body, "…") || len(se.Body) > maxErrorBody+len("…") {
		t.Errorf("body not truncated to %d bytes: %q", maxErrorBody, se.Body)
	}
}

func TestAckRequestBody(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/outbox/ack" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("Authorization: %q", got)
		}
		json.NewDecoder(r.Body).Decode(&raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := newTestClient(srv.URL).Ack(context.Background(), AckRequest{
		ClaimID: "c1",
		Results: []Result{
			{Key: "a", Status: StatusSent, MessageID: "3EB0ABC"},
			{Key: "b", Status: StatusFailed, Error: "send: timeout"},
			{Key: "c", Status: StatusRejected, Error: "text is not in the reminder layout"},
		},
	})
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
	want := `{"claimId":"c1","results":[{"key":"a","messageId":"3EB0ABC","status":"sent"},{"error":"send: timeout","key":"b","status":"failed"},{"error":"text is not in the reminder layout","key":"c","status":"rejected"}]}`
	got, _ := json.Marshal(raw)
	if string(got) != want {
		t.Errorf("ack body:\n got %s\nwant %s", got, want)
	}
}

func TestAckRetriesServerErrorsThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		switch calls.Add(1) {
		case 1:
			http.Error(w, "x", http.StatusBadGateway)
		case 2:
			http.Error(w, "x", http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var delays []int
	c.Backoff = func(attempt int) time.Duration { delays = append(delays, attempt); return 0 }
	err := c.Ack(context.Background(), AckRequest{ClaimID: "c", Results: []Result{{Key: "k", Status: StatusSent}}})
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("ack requests: got %d, want 3", n)
	}
	if len(delays) != 2 || delays[0] != 1 || delays[1] != 2 {
		t.Errorf("backoff attempts: got %v, want [1 2]", delays)
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("retry body differs: %q vs %q", b, bodies[0])
		}
	}
}

func TestAckGivesUpAfterThreeAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := newTestClient(srv.URL).Ack(context.Background(), AckRequest{ClaimID: "c"})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusServiceUnavailable {
		t.Fatalf("Ack error: got %v, want StatusError 503", err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("ack requests: got %d, want 3", n)
	}
}

func TestAckDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	if err := newTestClient(srv.URL).Ack(context.Background(), AckRequest{ClaimID: "c"}); err == nil {
		t.Fatal("Ack succeeded on 401")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("ack requests: got %d, want 1", n)
	}
}

type flakyTransport struct {
	failures atomic.Int32
	next     http.RoundTripper
}

func (f *flakyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if f.failures.Add(-1) >= 0 {
		return nil, errors.New("connection reset by peer")
	}
	return f.next.RoundTrip(r)
}

func TestAckRetriesTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ft := &flakyTransport{next: http.DefaultTransport}
	ft.failures.Store(2)
	c := newTestClient(srv.URL)
	c.HTTP = &http.Client{Transport: ft}
	if err := c.Ack(context.Background(), AckRequest{ClaimID: "c"}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
}

func TestRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	c := newTestClient(srv.URL)
	c.RequestTimeout = 50 * time.Millisecond
	start := time.Now()
	_, err := c.Claim(context.Background(), "c")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Claim: got %v, want deadline exceeded", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Claim took %v", d)
	}
}

func TestAckStopsWhenContextCancelled(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "x", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := newTestClient(srv.URL)
	c.Backoff = func(int) time.Duration { cancel(); return time.Hour }
	err := c.Ack(ctx, AckRequest{ClaimID: "c"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Ack: got %v, want context.Canceled", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("ack requests: got %d, want 1", n)
	}
}
