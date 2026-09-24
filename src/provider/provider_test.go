package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsIPURL(t *testing.T) {
	cases := map[string]bool{
		"http://94.156.167.30/path": true,
		"http://94.156.167.30/myfolder/INVOICE-4376746733738.r%e%g%r%nC%l%i%c%k%b%Y%e%s%b%To%b%Cancel%0.zip": true,
		"https://[2001:db8::1]/path":        true,
		"https://example.com/94.156.167.30": false,
		"http://999.156.167.30/path":        false,
	}

	for rawURL, expect := range cases {
		assert.Equal(t, expect, IsIPURL(rawURL))
	}
}
