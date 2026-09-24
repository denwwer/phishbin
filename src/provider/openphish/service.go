// Used free community feed https://openphish.com/phishing_feeds.html.
// Update frequency 12h

package openphish

import (
	"bufio"
	"context"
	"net/http"
	"strings"

	"github.com/phishbin/src/httpc"
	"github.com/phishbin/src/provider"
)

const dataURL = "https://raw.githubusercontent.com/openphish/public_feed/refs/heads/main/feed.txt"

type service struct{}

func New() provider.Provider {
	return &service{}
}

func (s service) Name() string {
	return "OpenPhish"
}

func (s service) Bit() uint32 {
	return 2
}

func (s service) Fetch(ctx context.Context, c *httpc.Client, emit func(urlData string)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dataURL, nil)
	if err != nil {
		return err
	}

	body, err := c.Send(ctx, req)
	if err != nil {
		return err
	}
	defer body.Close()

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 1<<20) // long URLs

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || provider.IsIPURL(line) {
			continue
		}

		emit(line)
	}
	return sc.Err()
}
