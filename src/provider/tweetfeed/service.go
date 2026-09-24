// URL's detected on twitter (x) https://tweetfeed.live/api/
// List updated daily.

package tweetfeed

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/phishbin/src/httpc"
	"github.com/phishbin/src/provider"
)

const dataURL = "https://api.tweetfeed.live/v1/today/url"

type response struct {
	Value string `json:"value"`
}

type service struct{}

func New() provider.Provider {
	return &service{}
}

func (s service) Name() string {
	return "TweetFeed"
}

func (s service) Bit() uint32 {
	return 4
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

	data := []response{}
	err = json.NewDecoder(body).Decode(&data)
	if err != nil {
		return err
	}

	for _, d := range data {
		if !provider.IsIPURL(d.Value) {
			emit(d.Value)
		}
	}

	return nil
}
