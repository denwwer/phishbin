package provider

import (
	"context"
	"net"
	"net/url"

	"github.com/phishbin/src/httpc"
)

type Provider interface {
	Name() string
	Bit() uint32 // unique bit for the src bitmask
	// Fetch streams raw URLs to emit. Must not buffer the whole feed.
	Fetch(ctx context.Context, c *httpc.Client, emit func(urlData string)) error
}

// Returns true if the given raw URL is an IP address, false otherwise.
func IsIPURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && net.ParseIP(u.Hostname()) != nil
}
