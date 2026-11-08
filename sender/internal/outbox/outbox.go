package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxResponseBytes = 1 << 20
	maxErrorBody     = 200
	ackAttempts      = 3
)

type Item struct {
	Key         string `json:"key"`
	Text        string `json:"text"`
	StartUTC    string `json:"startUtc"`
	DeadlineUTC string `json:"deadlineUtc"`
}

type ClaimRequest struct {
	ClaimID string `json:"claimId"`
}

type ClaimResponse struct {
	Items []Item `json:"items"`
}

type Status string

const (
	StatusSent     Status = "sent"
	StatusFailed   Status = "failed"
	StatusRejected Status = "rejected"
)

type Result struct {
	Key       string `json:"key"`
	Status    Status `json:"status"`
	MessageID string `json:"messageId,omitempty"`
	Error     string `json:"error,omitempty"`
}

type AckRequest struct {
	ClaimID string   `json:"claimId"`
	Results []Result `json:"results"`
}

type StatusError struct {
	Op   string
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("outbox %s: HTTP %d: %s", e.Op, e.Code, e.Body)
}

func (e *StatusError) retryable() bool {
	return e.Code == http.StatusTooManyRequests || e.Code >= 500
}

type Client struct {
	BaseURL        string
	Token          string
	HTTP           *http.Client
	RequestTimeout time.Duration
	Backoff        func(attempt int) time.Duration
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL:        strings.TrimRight(baseURL, "/"),
		Token:          token,
		HTTP:           http.DefaultClient,
		RequestTimeout: 15 * time.Second,
		Backoff:        func(attempt int) time.Duration { return time.Second << (attempt - 1) },
	}
}

func (c *Client) Claim(ctx context.Context, claimID string) ([]Item, error) {
	var resp ClaimResponse
	if err := c.post(ctx, "claim", ClaimRequest{ClaimID: claimID}, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (c *Client) Ack(ctx context.Context, req AckRequest) error {
	var err error
	for attempt := 1; attempt <= ackAttempts; attempt++ {
		if attempt > 1 {
			if serr := sleep(ctx, c.Backoff(attempt-1)); serr != nil {
				return fmt.Errorf("outbox ack: %w", errors.Join(err, serr))
			}
		}
		err = c.post(ctx, "ack", req, nil)
		if err == nil || !retryable(ctx, err) {
			return err
		}
	}
	return err
}

func (c *Client) post(ctx context.Context, op string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("outbox %s: encode: %w", op, err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/outbox/"+op, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("outbox %s: %w", op, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("outbox %s: %w", op, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("outbox %s: read response: %w", op, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &StatusError{Op: op, Code: resp.StatusCode, Body: truncate(strings.TrimSpace(string(data)), maxErrorBody)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("outbox %s: decode response: %w", op, err)
	}
	return nil
}

func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if se, ok := errors.AsType[*StatusError](err); ok {
		return se.retryable()
	}
	return true
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
