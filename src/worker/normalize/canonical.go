// Package normalize turns a URL into the canonical "host/path[?query]" pattern
// stored in D1, and expands a looked-up URL into its host-suffix/path-prefix
// candidate patterns.
//
// The percent-encoding, IP-literal and candidate-expansion logic is copied from
// github.com/google/safebrowsing (see sb_urls.go). Parsing is done with the
// host language's URL parser so that this file and worker/src/canonical.ts
// produce byte-identical output. Both are tested against testdata/normalize.json.
package normalize

import (
	"crypto/sha256"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

var (
	// ErrInvalid means the string is not a usable http(s) URL.
	ErrInvalid = errors.New("normalize: invalid url")
	// ErrIPHost means the host is an IP literal. Feeds skip these; the Worker
	// rejects them (legitimate short links do not use bare IPs).
	ErrIPHost = errors.New("normalize: ip literal host")
)

var (
	dotsRegexp          = regexp.MustCompile(`[.]+`)
	possibleIPRegexp    = regexp.MustCompile(`^(?i)((?:0x[0-9a-f]+|[0-9\.])+)$`)
	trailingSpaceRegexp = regexp.MustCompile(`^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}) `)

	// schemeRegexp captures an explicit "scheme://" prefix, so that
	// "evil.com:8080/x" is treated as schemeless rather than as scheme "evil.com".
	schemeRegexp = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*)://`)

	// hostRegexp must match HOST_RE in canonical.ts. It requires at least two
	// labels and a TLD starting with a letter, so numeric hosts never pass.
	hostRegexp = regexp.MustCompile(`^(?:[a-z0-9_](?:[a-z0-9_-]{0,61}[a-z0-9_])?\.)+[a-z][a-z0-9-]{0,62}$`)

	// UTS-46 non-transitional, lenient, matching the WHATWG URL parser.
	idn = idna.New(
		idna.MapForLookup(),
		idna.Transitional(false),
		idna.StrictDomainName(false),
		idna.CheckHyphens(false),
	)
)

// Parts is a parsed, canonicalized URL. Scheme, port, userinfo and fragment are
// dropped: they never affect which page is served.
type Parts struct {
	Host  string // lowercase, punycode, no trailing dot
	Path  string // canonical, always starts with "/"
	Query string // canonical, without the leading "?"
}

// Pattern is the canonical string that gets hashed and stored.
func (p Parts) Pattern() string {
	if p.Query != "" {
		return p.Host + p.Path + "?" + p.Query
	}
	return p.Host + p.Path
}

// Hash is the key stored in D1 (bad_url.h).
func Hash(pattern string) [32]byte { return sha256.Sum256([]byte(pattern)) }

// Canonical is the feed-side entry point: one URL in, one pattern out.
func Canonical(raw string) (string, error) {
	p, err := Parse(raw)
	if err != nil {
		return "", err
	}
	return p.Pattern(), nil
}

// Candidates is the lookup-side entry point: every pattern that, if present in
// the blocklist, should block raw. Up to 7 hosts x 6 paths, deduplicated.
func Candidates(p Parts) []string {
	hosts := generateLookupHosts(p.Host, false)
	paths := generateLookupPaths(p.Path, p.Query)

	out := make([]string, 0, len(hosts)*len(paths))
	seen := make(map[string]struct{}, len(hosts)*len(paths))
	for _, h := range hosts {
		for _, pa := range paths {
			c := h + pa
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			out = append(out, c)
		}
	}
	return out
}

// Parse canonicalizes raw. Keep every step in sync with parse() in canonical.ts.
//
// This is the feed side, so it is deliberately scheme-agnostic: it accepts http
// and https alike and drops the scheme from the pattern. Feeds are full of http
// URLs, and a feed entry "http://evil.com/x" must block a user's
// "https://evil.com/x". The shortener's https-only rule is user-input policy and
// lives in parseUserUrl() in the Worker, not here.
func Parse(raw string) (Parts, error) {
	pre, ok := preprocess(raw)
	if !ok {
		return Parts{}, ErrInvalid
	}
	u, err := url.Parse(pre)
	if err != nil {
		return Parts{}, ErrInvalid
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Parts{}, ErrInvalid
	}
	if strings.Contains(u.Host, "[") { // IPv6 literal
		return Parts{}, ErrIPHost
	}

	host, err := canonHost(u.Hostname())
	if err != nil {
		return Parts{}, err
	}

	path, err := canonPath(u.EscapedPath())
	if err != nil {
		return Parts{}, ErrInvalid
	}

	query, err := recursiveUnescape(u.RawQuery)
	if err != nil {
		return Parts{}, ErrInvalid
	}

	return Parts{Host: host, Path: path, Query: escapeQuery(query)}, nil
}

// preprocess mirrors what the WHATWG parser does before parsing, so that
// net/url sees the same string the Worker's `new URL()` sees. ok is false when
// the WHATWG host parser would have thrown.
func preprocess(raw string) (string, bool) {
	if i := strings.IndexByte(raw, '#'); i >= 0 { // drop the fragment
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	raw = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(raw)
	raw = strings.ReplaceAll(raw, `\`, "/") // special schemes treat \ as /
	raw = escapeLonePercent(raw)            // net/url rejects "%zz"; WHATWG encodes it

	m := schemeRegexp.FindStringSubmatch(raw)
	if m == nil {
		raw = "http://" + raw // some feeds ship bare "evil.com/x"
	} else if s := strings.ToLower(m[1]); s == "ftps" || s == "sftp" {
		// ftps and sftp carry a host and path like https does, and the scheme is
		// not part of the pattern, so rewriting is lossless. It is also required:
		// the WHATWG parser treats them as non-special schemes and would hand the
		// Worker an opaque host — not lowercased, not percent-decoded, no IDNA.
		raw = "https://" + raw[len(m[0]):]
	}

	return decodeAuthority(raw)
}

// forbiddenHostBytes are the WHATWG forbidden host code points. '%' is included
// because the host is decoded exactly once, so a surviving '%' is invalid.
const forbiddenHostBytes = "\x00\t\n\r #%/:<>?@[\\]^|"

// decodeAuthority percent-decodes the authority once and reports whether every
// decoded byte is allowed in a host, which is what the WHATWG host parser does.
// net/url instead rejects any escape in the host at all ("invalid URL escape"),
// so without this Go would drop "https://ev%69l.com/x" while the Worker resolved
// it to evil.com — the blocklist would miss an entry the lookup side canonicalizes.
func decodeAuthority(raw string) (string, bool) {
	m := schemeRegexp.FindStringSubmatch(raw)
	if m == nil {
		return raw, true
	}
	start := len(m[0])
	end := len(raw)
	if i := strings.IndexAny(raw[start:], "/?#"); i >= 0 {
		end = start + i
	}
	auth := raw[start:end]
	if !strings.Contains(auth, "%") {
		return raw, true
	}

	var b strings.Builder
	b.Grow(len(auth))
	for i := 0; i < len(auth); i++ {
		if auth[i] == '%' && i+2 < len(auth) && isHex(auth[i+1]) && isHex(auth[i+2]) {
			c := unhex(auth[i+1])<<4 | unhex(auth[i+2])
			if strings.IndexByte(forbiddenHostBytes, c) >= 0 {
				return "", false
			}
			b.WriteByte(c)
			i += 2
			continue
		}
		b.WriteByte(auth[i])
	}
	return raw[:start] + b.String() + raw[end:], true
}

// escapeLonePercent replaces a '%' that does not start a valid escape with
// "%25", which is what the WHATWG parser does. Without it net/url would reject
// the URL while the Worker happily canonicalized it.
func escapeLonePercent(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && !(i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2])) {
			b.WriteString("%25")
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// canonHost takes the host after preprocess/decodeAuthority has already done the
// single percent-decoding pass, exactly as WHATWG hands one to the Worker.
func canonHost(h string) (string, error) {
	if isUnicode(h) {
		a, err := idn.ToASCII(h)
		if err != nil {
			return "", ErrInvalid
		}
		h = a
	}
	h = dotsRegexp.ReplaceAllString(h, ".")
	h = strings.Trim(h, ".")
	h = strings.ToLower(h)

	if ip := parseIPAddress(h); ip != "" {
		return "", ErrIPHost
	}
	if !hostRegexp.MatchString(h) {
		return "", ErrInvalid
	}
	return h, nil
}

// canonPath decodes the path, resolves "." / ".." / empty segments, then escapes
// exactly once. Equivalent to path.Clean plus Google's trailing-slash rule, but
// written as an explicit loop so the TypeScript port is line-for-line the same.
func canonPath(p string) (string, error) {
	p, err := recursiveUnescape(p)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}

	segs := strings.Split(p[1:], "/")
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		switch s {
		case "", ".": // empty and "." segments vanish
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, s)
		}
	}

	res := "/" + strings.Join(out, "/")
	if last := segs[len(segs)-1]; len(out) > 0 && (last == "" || last == "." || last == "..") {
		res += "/" // the original ended on a directory
	}
	return escapePath(res), nil
}
