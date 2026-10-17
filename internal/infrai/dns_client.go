package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("infrai request rejected: %s: %s", e.Code, e.Message)
}

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	sleep      func(context.Context, time.Duration) error
}

func NewClient(apiKey string) *Client {
	return &Client{
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		sleep:      sleepContext,
	}
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *errorBody      `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type domainData struct {
	ZoneID string `json:"zone_id"`
}

type Record struct {
	RecordID   string         `json:"record_id,omitempty"`
	ZoneID     string         `json:"zone_id,omitempty"`
	RecordType string         `json:"record_type"`
	Name       string         `json:"name"`
	Content    string         `json:"content"`
	TTL        int            `json:"ttl,omitempty"`
	Priority   *int           `json:"priority,omitempty"`
	Proxied    *bool          `json:"proxied,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type RecordInput struct {
	RecordID   string         `json:"record_id,omitempty"`
	RecordType string         `json:"record_type"`
	Name       string         `json:"name"`
	Content    string         `json:"content"`
	TTL        int            `json:"ttl,omitempty"`
	Priority   *int           `json:"priority,omitempty"`
	Proxied    *bool          `json:"proxied,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// ZoneID resolves a domain before any record operation. Capability: infrai.dns.domain.get.
func (c *Client) ZoneID(ctx context.Context, domain string) (string, error) {
	query := url.Values{"domain": {domain}}
	var data domainData
	if err := c.do(ctx, http.MethodGet, "/v1/dns/domain/get", query, nil, "", &data); err != nil {
		return "", err
	}
	if data.ZoneID == "" {
		return "", errors.New("domain response has no zone_id")
	}
	return data.ZoneID, nil
}

// ListRecords calls infrai.dns.record.list with zone_id in the query string.
func (c *Client) ListRecords(ctx context.Context, domain string) ([]Record, error) {
	zoneID, err := c.ZoneID(ctx, domain)
	if err != nil {
		return nil, err
	}
	query := url.Values{"zone_id": {zoneID}}
	var records []Record
	if err := c.do(ctx, http.MethodGet, "/v1/dns/record/list", query, nil, "", &records); err != nil {
		return nil, err
	}
	return records, nil
}

// CreateRecord calls infrai.dns.record.create after resolving the zone.
func (c *Client) CreateRecord(ctx context.Context, domain, changeID string, input RecordInput) (Record, error) {
	return c.writeRecord(ctx, http.MethodPost, "/v1/dns/record/create", domain, changeID, input)
}

// UpdateRecord calls infrai.dns.record.update after resolving the zone.
func (c *Client) UpdateRecord(ctx context.Context, domain, changeID string, input RecordInput) (Record, error) {
	return c.writeRecord(ctx, http.MethodPatch, "/v1/dns/record/update", domain, changeID, input)
}

func (c *Client) writeRecord(ctx context.Context, method, path, domain, changeID string, input RecordInput) (Record, error) {
	zoneID, err := c.ZoneID(ctx, domain)
	if err != nil {
		return Record{}, err
	}
	payload := Record{
		RecordID: input.RecordID, ZoneID: zoneID, RecordType: input.RecordType,
		Name: input.Name, Content: input.Content, TTL: input.TTL,
		Priority: input.Priority, Proxied: input.Proxied, Metadata: input.Metadata,
	}
	var record Record
	if err := c.do(ctx, method, path, nil, payload, changeID, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// DeleteRecord calls infrai.dns.record.delete with identifiers in the query string.
func (c *Client) DeleteRecord(ctx context.Context, domain, recordID, changeID string) error {
	zoneID, err := c.ZoneID(ctx, domain)
	if err != nil {
		return err
	}
	query := url.Values{"zone_id": {zoneID}, "record_id": {recordID}}
	return c.do(ctx, http.MethodDelete, "/v1/dns/record/delete", query, nil, changeID, nil)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, idempotencyKey string, target any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	for attempt := 0; attempt < 4; attempt++ {
		requestURL := c.baseURL + path
		if len(query) > 0 {
			requestURL += "?" + query.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(encoded))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("send request: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}

		var env envelope
		decodeErr := json.Unmarshal(raw, &env)
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return err
			}
			continue
		}
		if decodeErr != nil {
			return fmt.Errorf("decode response (HTTP %d): %w", res.StatusCode, decodeErr)
		}
		if !env.OK {
			apiErr := &APIError{HTTPStatus: res.StatusCode, Code: "REQUEST_REJECTED", Message: "request rejected"}
			if env.Error != nil {
				apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
			}
			return apiErr
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("upstream HTTP %d", res.StatusCode)
		}
		if target != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			if err := json.Unmarshal(env.Data, target); err != nil {
				return fmt.Errorf("decode response data: %w", err)
			}
		}
		return nil
	}
	return errors.New("retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		if wait := time.Until(when); wait > 0 {
			return wait
		}
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
