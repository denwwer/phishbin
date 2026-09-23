// Download Plain-Text URL List (URLs only) for past 30 days.
// https://urlhaus.abuse.ch/api/#plain-text

package urlhaus

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/phishbin/src/httpc"
	"github.com/phishbin/src/provider"
)

const dataURL = "https://urlhaus-api.abuse.ch/v2/files/exports/%s/urls_recent.txt"

type service struct {
	authKey string
}

func New(authKey string) provider.Provider {
	return &service{authKey: authKey}
}

func (s service) Name() string {
	return "URLhaus"
}

func (s service) Bit() uint32 {
	return 1
}

func (s service) Fetch(ctx context.Context, c *httpc.Client, emit func(urlData string)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(dataURL, s.authKey), nil)
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
		if line == "" || line[0] == '#' {
			continue
		}

		emit(line)
	}
	return sc.Err()
}
