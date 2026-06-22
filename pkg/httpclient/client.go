// Package httpclient is a tiny wrapper around net/http: timeout, JSON
// helpers, retry-on-5xx. Used by lib/coreclient to talk to core-service.
//
// LAYERING: pkg/. No imports from lib/ or app/.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Config struct {
	Timeout    time.Duration
	MaxRetries int
}

type Client struct {
	httpc *http.Client
	cfg   Config
}

func New(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 2
	}
	return &Client{
		httpc: &http.Client{Timeout: cfg.Timeout},
		cfg:   cfg,
	}
}

// DoJSON issues req and decodes the JSON response into out (if non-nil).
// Retries on 5xx with exponential backoff. Caller sets headers BEFORE calling.
func (c *Client) DoJSON(ctx context.Context, method, url string, headers map[string]string, body, out any) (int, error) {
	var (
		resp     *http.Response
		lastErr  error
		bodyBuf  []byte
		err      error
		attempts = c.cfg.MaxRetries + 1
	)
	if body != nil {
		bodyBuf, err = json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("marshal body: %w", err)
		}
	}

	for attempt := 0; attempt < attempts; attempt++ {
		var rdr io.Reader
		if bodyBuf != nil {
			rdr = bytes.NewReader(bodyBuf)
		}
		req, rerr := http.NewRequestWithContext(ctx, method, url, rdr)
		if rerr != nil {
			return 0, fmt.Errorf("build request: %w", rerr)
		}
		req.Header.Set("Accept", "application/json")
		if bodyBuf != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, lastErr = c.httpc.Do(req)
		if lastErr != nil {
			if attempt+1 < attempts {
				time.Sleep(backoff(attempt))
				continue
			}
			return 0, fmt.Errorf("http do: %w", lastErr)
		}
		if resp.StatusCode >= 500 && attempt+1 < attempts {
			_ = resp.Body.Close()
			time.Sleep(backoff(attempt))
			continue
		}
		break
	}

	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 400 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

func backoff(attempt int) time.Duration {
	return time.Duration(100*(1<<attempt)) * time.Millisecond
}
