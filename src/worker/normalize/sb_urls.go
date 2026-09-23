// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This file contains code copied from github.com/google/safebrowsing
// (urls.go, Apache-2.0). MODIFICATIONS relative to the original:
//
//  1. Google's custom parseURL/parseHost/canonicalURL are NOT copied. Parsing is
//     done by the host language's URL parser instead (net/url here,
//     WHATWG `new URL()` in the Worker) so the Go and TypeScript sides agree.
//     See canonical.go.
//  2. escapePath escapes '?' in addition to Google's set. Google unescapes the
//     whole URL before splitting, so a decoded '?' always starts the query;
//     we split first, so a '?' decoded inside the path must be re-escaped or
//     the canonical string would be ambiguous. escapeQuery is Google's set.
//  3. generateLookupHosts/generateLookupPaths take already-parsed parts instead
//     of a URL string, and return no error.
//  4. Unused helpers (split, getScheme, canonicalPath, ValidURL, generateHashes)
//     are omitted.
//
// Any change here must be mirrored in worker/src/sb_urls.ts.
package normalize

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// isHex reports whether c is a hexadecimal character.
func isHex(c byte) bool {
	switch {
	case '0' <= c && c <= '9':
		return true
	case 'a' <= c && c <= 'f':
		return true
	case 'A' <= c && c <= 'F':
		return true
	}
	return false
}

// unhex converts a hexadecimal character to byte value in 0..15, inclusive.
func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// isUnicode reports whether s is a Unicode string.
func isUnicode(s string) bool {
	for _, c := range []byte(s) {
		// For legacy reasons, 0x80 is not considered a Unicode character.
		if c > 0x80 {
			return true
		}
	}
	return false
}

// escape returns the percent-encoded form of the string s.
// MODIFIED: takes the extra character set as a parameter (see escapePath).
func escape(s string, extra string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range []byte(s) {
		if c < 0x20 || c >= 0x7f || c == ' ' || c == '#' || c == '%' ||
			strings.IndexByte(extra, c) >= 0 {
			fmt.Fprintf(&b, "%%%02x", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func escapePath(s string) string  { return escape(s, "?") }
func escapeQuery(s string) string { return escape(s, "") }

// unescape returns the decoded form of a percent-encoded string s.
func unescape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		if len(s) >= 3 && s[0] == '%' && isHex(s[1]) && isHex(s[2]) {
			b.WriteByte(unhex(s[1])<<4 | unhex(s[2]))
			s = s[3:]
		} else {
			b.WriteByte(s[0])
			s = s[1:]
		}
	}
	return b.String()
}

// recursiveUnescape unescapes the string s recursively until it cannot be
// unescaped anymore. It reports an error if the unescaping process seemed to
// have no end.
func recursiveUnescape(s string) (string, error) {
	const maxDepth = 1024
	for i := 0; i < maxDepth; i++ {
		t := unescape(s)
		if t == s {
			return s, nil
		}
		s = t
	}
	return "", errors.New("normalize: unescaping is too recursive")
}

// parseIPAddress returns the canonical dotted-decimal form of iphostname, or ""
// if it is not an IP address. It handles the hex, octal and short forms that
// legacy resolvers accept (0x7f000001, 127.1, "10.192.95.89 xy").
func parseIPAddress(iphostname string) string {
	// The Windows resolver allows a 4-part dotted decimal IP address to have a
	// space followed by any old rubbish, so long as the total length of the
	// string doesn't get above 15 characters. So, "10.192.95.89 xy" is
	// resolved to 10.192.95.89. If the string length is greater than 15
	// characters, e.g. "10.192.95.89 xy.wildcard.example.com", it will be
	// resolved through DNS.
	if len(iphostname) <= 15 {
		match := trailingSpaceRegexp.FindString(iphostname)
		if match != "" {
			iphostname = strings.TrimSpace(match)
		}
	}
	if !possibleIPRegexp.MatchString(iphostname) {
		return ""
	}

	parts := strings.Split(iphostname, ".")
	if len(parts) > 4 {
		return ""
	}
	ss := make([]string, len(parts))
	for i, n := range parts {
		if i == len(parts)-1 {
			ss[i] = canonicalNum(n, 5-len(parts))
		} else {
			ss[i] = canonicalNum(n, 1)
		}
		if ss[i] == "" {
			return ""
		}
	}
	return strings.Join(ss, ".")
}

// canonicalNum parses s as an integer and attempts to encode it as a '.'
// separated string where each element is the base-10 encoded value of each byte
// for the corresponding number, starting with the MSB. The result is one that
// is usable as an IP address.
//
// For example:
//
//	s:"01234",      n:2 => "2.156"
//	s:"0x10203040", n:4 => "16.32.48.64"
func canonicalNum(s string, n int) string {
	if n <= 0 || n > 4 {
		return ""
	}
	v, err := strconv.ParseUint(s, 0, 32)
	if err != nil {
		return ""
	}
	ss := make([]string, n)
	for i := n - 1; i >= 0; i-- {
		ss[i] = strconv.Itoa(int(v) & 0xff)
		v = v >> 8
	}
	return strings.Join(ss, ".")
}

// generateLookupHosts returns a list of host-suffixes for the parsed host.
//
// Safe Browsing policy asks to generate lookup hosts for the URL. Those are
// formed by the domain and also up to 4 hostname suffixes. The last component
// or sometimes the pair isn't examined alone, since it's the TLD or country
// code.
//
// Note that we do not need to be clever about stopping at the "real" TLD.
// We just check a few extra components regardless.
func generateLookupHosts(host string, isIP bool) []string {
	const maxHostComponents = 7

	if isIP {
		return []string{host}
	}

	hostComponents := strings.Split(host, ".")
	numComponents := len(hostComponents) - maxHostComponents
	if numComponents < 1 {
		numComponents = 1
	}

	hosts := []string{host}
	for i := numComponents; i < len(hostComponents)-1; i++ {
		hosts = append(hosts, strings.Join(hostComponents[i:], "."))
	}
	return hosts
}

// generateLookupPaths returns a list of path-prefixes for the parsed path/query.
func generateLookupPaths(path, query string) []string {
	const maxPathComponents = 4

	paths := []string{"/"}
	var pathComponents []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			pathComponents = append(pathComponents, p)
		}
	}

	numComponents := len(pathComponents)
	if numComponents > maxPathComponents {
		numComponents = maxPathComponents
	}
	for i := 1; i < numComponents; i++ {
		paths = append(paths, "/"+strings.Join(pathComponents[:i], "/")+"/")
	}
	if path != "/" {
		paths = append(paths, path)
	}
	if len(query) > 0 {
		paths = append(paths, path+"?"+query)
	}
	return paths
}
