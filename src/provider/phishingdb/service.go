// Used phishing-links-INACTIVE feed https://github.com/Phishing-Database/Phishing.Database.
// File is updated regularly (that repo said).

package phishingdb

import (
	"bufio"
	"context"
	"net/http"
	"strings"

	"github.com/phishbin/src/httpc"
	"github.com/phishbin/src/provider"
)

const dataURL = "https://phish.co.za/latest/phishing-links-ACTIVE.txt"

type service struct{}

func New() provider.Provider {
	return &service{}
}

func (s service) Name() string {
	return "PhishingDatabase"
}

func (s service) Bit() uint32 {
	return 5
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
