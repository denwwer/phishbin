# phishbin

Syncs public phishing and malware URL feeds into Cloudflare D1 as hashed, canonical URLs. Any service can query D1 to check whether a URL is known-bad.

```
feeds ──► Go sync (Docker) ──► diff ──► D1 ◄── your service (lookup)
             └── local SQLite (current / prev)
```

## Design

- **Free, commercial-safe feeds:** URLhaus, ThreatFox, PhishStats, Phishing.Database, TweetFeed, PhishTank.
- **Hashes only:** D1 stores SHA-256 hashes of canonical URLs, never raw URLs.
- **Diff-only sync:** only the changes are written.
- **Append-only `bad_url`:** pruned periodically, so a failing feed never causes mass deletes.

## Pipeline

1. Fetch all feeds concurrently.
2. Canonicalize every URL and hash it.
3. Write the hashes to the local `current` table.
4. Diff `current` against `prev`.
5. Apply the diff to D1 in chunks.
6. Write the worker result to the `status` table.
7. Rotate `current` to `prev`, only if every chunk succeeded.

A failed run recomputes the same diff on the next run. If local state is lost - rebuilds.

## Run

```sh
docker build -t phishbin .
docker run -d --restart unless-stopped -v phishbin-data:/data --env-file .env phishbin
```

Configure it with env vars `.env.example`
