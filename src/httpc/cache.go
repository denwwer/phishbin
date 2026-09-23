package httpc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

type Transport struct {
	Base http.RoundTripper
	Dir  string
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	key := filepath.Join(t.Dir, req.URL.Host, cacheKey(req.URL.String()))

	data, err := os.ReadFile(key)
	if err == nil {
		slog.Warn("cache hit", slog.String("host", req.URL.Host))
		return cachedResponse(req, data), nil
	}

	if !os.IsNotExist(err) {
		return nil, err
	}

	resp, err := base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusOK {
		if err := os.MkdirAll(filepath.Dir(key), 0755); err != nil {
			return nil, err
		}

		if err := os.WriteFile(key, data, 0644); err != nil {
			return nil, err
		}
	}

	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))

	return resp, nil
}

func cachedResponse(req *http.Request, data []byte) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: int64(len(data)),
		Header:        make(http.Header),
		Request:       req,
	}
}

func cacheKey(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:])
}
