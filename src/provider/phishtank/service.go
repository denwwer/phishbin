// Used CSV database https://phishtank.net/developer_info.php.
// Update updated hourly.
// CSV headers:
// [phish_id,url,phish_detail_url,submission_time,verified,..]
// we interesting only in [1] - url, [4] - verified (yes)

package phishtank

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"io"
	"net/http"

	"github.com/phishbin/src/httpc"
	"github.com/phishbin/src/provider"
)

const dataURL = "http://data.phishtank.com/data/online-valid.csv.gz"

type service struct{}

func New() provider.Provider {
	return &service{}
}

func (s service) Name() string {
	return "PhishTank"
}

func (s service) Bit() uint32 {
	return 3
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

	buff := &bytes.Buffer{}
	buff.Grow(1 << 20)

	err = unzip(body, buff)
	if err != nil {
		return err
	}

	sc := csv.NewReader(buff)
	sc.FieldsPerRecord = -1 // any counts
	sc.LazyQuotes = true

	for {
		rec, err := sc.Read()
		if err == io.EOF {
			return nil // end
		}

		if err != nil {
			return err
		}

		// "url" - header row
		if rec[1] != "url" && rec[4] == "yes" && !provider.IsIPURL(rec[1]) {
			emit(rec[1])
		}
	}
}

func unzip(body io.ReadCloser, w io.Writer) error {
	gz, err := gzip.NewReader(body)
	if err != nil {
		return err
	}
	defer gz.Close()

	_, err = io.Copy(w, gz)
	return err
}
