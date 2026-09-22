package httpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const sendMaxAttempts = 2

type Client struct {
	cl *http.Client
}

// New creates a new HTTP client with retries.
func New() *Client {
	cl := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:    10,
			IdleConnTimeout: 30 * time.Second,
		},
		Timeout: time.Second * 60,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	return &Client{cl: cl}
}

// Send execute request and returns its body.
// Ensure resp.Body is closed on all execution paths.
func (c *Client) Send(ctx context.Context, req *http.Request) (io.ReadCloser, error) {
	var lastErr error

	for attempt := 0; attempt < sendMaxAttempts; attempt++ {
		if attempt > 0 {
			slog.WarnContext(ctx, fmt.Sprintf("attempt step %d", attempt))
			time.Sleep(time.Minute * time.Duration(1<<attempt)) // 2s, 4s, etc
		}

		resp, err := c.cl.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		status := resp.StatusCode

		if status < 300 {
			return resp.Body, nil
		}

		lastErr = fmt.Errorf(`status: %s`, resp.Status)
	}

	return nil, lastErr
}
