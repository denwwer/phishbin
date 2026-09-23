package normalize

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// fixtures is the contract between this package and worker/src/canonical.ts.
// Both must pass it; if they disagree, lookups silently stop matching.
type fixtures struct {
	Canonical []struct {
		In  string  `json:"in"`
		Out *string `json:"out"` // null = must be rejected
	} `json:"canonical"`
	Candidates []struct {
		In  string   `json:"in"`
		Out []string `json:"out"` // sorted
	} `json:"candidates"`
}

func load(t *testing.T) fixtures {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/normalize.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixtures
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCanonical(t *testing.T) {
	for _, c := range load(t).Canonical {
		got, err := Canonical(c.In)
		switch {
		case c.Out == nil && err == nil:
			t.Errorf("Canonical(%q) = %q, want rejected", c.In, got)
		case c.Out != nil && err != nil:
			t.Errorf("Canonical(%q) failed: %v, want %q", c.In, err, *c.Out)
		case c.Out != nil && got != *c.Out:
			t.Errorf("Canonical(%q) = %q, want %q", c.In, got, *c.Out)
		}
	}
}

func TestCandidates(t *testing.T) {
	for _, c := range load(t).Candidates {
		p, err := Parse(c.In)
		if err != nil {
			t.Errorf("Parse(%q) failed: %v", c.In, err)
			continue
		}
		got := Candidates(p)
		sort.Strings(got)
		if len(got) != len(c.Out) {
			t.Errorf("Candidates(%q) = %d patterns, want %d\n got %v\nwant %v",
				c.In, len(got), len(c.Out), got, c.Out)
			continue
		}
		for i := range got {
			if got[i] != c.Out[i] {
				t.Errorf("Candidates(%q)[%d] = %q, want %q", c.In, i, got[i], c.Out[i])
			}
		}
	}
}

// A stored pattern must always be one of the candidates generated for the same
// URL, otherwise a feed entry could never match itself.
func TestCandidatesContainOwnPattern(t *testing.T) {
	for _, c := range load(t).Canonical {
		if c.Out == nil {
			continue
		}
		p, err := Parse(c.In)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.In, err)
		}
		found := false
		for _, cand := range Candidates(p) {
			if cand == *c.Out {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Candidates(%q) does not contain its own pattern %q", c.In, *c.Out)
		}
	}
}

func TestRecursiveUnescapeTerminates(t *testing.T) {
	// "%25" decodes to "%", so this shrinks by one each pass and terminates.
	in := "%252525252525252541"
	got, err := recursiveUnescape(in)
	if err != nil {
		t.Fatal(err)
	}
	if got != "A" {
		t.Errorf("recursiveUnescape(%q) = %q, want %q", in, got, "A")
	}
}

func TestParseIPAddress(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4":         "1.2.3.4",
		"0x7f000001":      "127.0.0.1",
		"127.1":           "127.0.0.1",
		"012.034.01.055":  "10.28.1.45",
		"10.192.95.89 xy": "10.192.95.89",
		"evil.com":        "",
		"1.2.3.4.5":       "",
	}
	for in, want := range cases {
		if got := parseIPAddress(in); got != want {
			t.Errorf("parseIPAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
