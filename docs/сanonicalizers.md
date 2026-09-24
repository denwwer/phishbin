## Canonicalizers

`src/worker/internal/normalize/canonical.go` (Go) and `worker/src/canonical.ts` (TypeScript) implement
the same algorithm twice, on purpose.

Blocklist entries are stored as `sha256(pattern)`, not as text, so matching is
byte-exact: one differing byte and the lookup finds nothing — silently, with no
error anywhere. The two sides sit in different runtimes and cannot share code:

- **Go, feed side.** Runs in the sync service. Turns each feed URL into one
  canonical pattern and writes its hash to D1.
- **TypeScript, lookup side.** Runs in the Worker. Turns
  the submitted URL into its host-suffix / path-prefix candidates and asks D1
  about all of them at once.

Canonicalization is not optional on either side. Feeds disagree about scheme,
case, trailing dots and percent-encoding, and a submitted URL is attacker-
controlled: `EVIL.com:443/./log%69n.php#x` must hash the same as
`evil.com/login.php`, or the check is bypassed by typing it differently.

`testdata/normalize.json` is the contract that keeps them in step. Both test
suites assert against it, so a change to one side fails the other side's tests
instead of quietly disabling the blocklist.
