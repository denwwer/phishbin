// Canonicalization + candidate expansion for the URL blocklist lookup.
// Mirror of internal/normalize/canonical.go. Both are tested against
// testdata/normalize.json; if they drift, lookups silently stop matching.

import {
  escapePath,
  escapeQuery,
  generateLookupHosts,
  generateLookupPaths,
  parseIPAddress,
  recursiveUnescape,
  unescape,
} from './sb_urls';

export type UrlErrorCode = 'invalid_host' | 'ip_not_allowed';

export class InvalidUrlError extends Error {
  constructor(public readonly code: UrlErrorCode) {
    super(code);
  }
}

/** Must match hostRegexp in canonical.go. */
const HOST_RE = /^(?:[a-z0-9_](?:[a-z0-9_-]{0,61}[a-z0-9_])?\.)+[a-z][a-z0-9-]{0,62}$/;

/** Only an explicit "scheme://" counts, so "evil.com:8080/x" stays schemeless. */
const SCHEME_RE = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//;

export interface Parts {
  host: string;  // lowercase, punycode, no trailing dot
  path: string;  // canonical, always starts with "/"
  query: string; // canonical, without the leading "?"
}

/** The canonical string that gets hashed and stored. */
export function pattern(p: Parts): string {
  return p.query ? p.host + p.path + '?' + p.query : p.host + p.path;
}

/**
 * Canonicalize, accepting http and https alike. Twin of Parse() in canonical.go.
 *
 * The scheme is dropped from the pattern, so a feed entry "http://evil.com/x"
 * matches a submitted "https://evil.com/x". Rejecting http is the submit
 * endpoint's job, not this function's.
 */
export function parse(raw: string): Parts {
  let u: URL;
  try {
    u = new URL(preprocess(raw));
  } catch {
    throw new InvalidUrlError('invalid_host');
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') {
    throw new InvalidUrlError('invalid_host');
  }
  if (u.hostname.startsWith('[')) throw new InvalidUrlError('ip_not_allowed'); // IPv6

  return {
    host: canonHost(u.hostname),
    path: canonPath(u.pathname),
    query: escapeQuery(recursiveUnescape(u.search.slice(1))),
  };
}

/**
 * Every pattern that, if present in the blocklist, should block this URL.
 * Up to 7 hosts x 6 paths, deduplicated.
 */
export function candidates(p: Parts): string[] {
  const hosts = generateLookupHosts(p.host, false);
  const paths = generateLookupPaths(p.path, p.query);

  const out = new Set<string>();
  for (const h of hosts) for (const pa of paths) out.add(h + pa);
  return [...out];
}

const enc = new TextEncoder();

/** One indexed D1 query for all candidates. Throws InvalidUrlError on bad input. */
export async function isListed(db: D1Database, rawUrl: string): Promise<boolean> {
  const cands = candidates(parse(rawUrl));
  const hashes = await Promise.all(
    cands.map(async (c) => new Uint8Array(await crypto.subtle.digest('SHA-256', enc.encode(c)))),
  );
  const sql = `SELECT 1 FROM bad_url WHERE h IN (${hashes.map(() => '?').join(',')}) LIMIT 1`;
  return (await db.prepare(sql).bind(...hashes).first()) !== null;
}

// --- internals ---

/** Mirrors preprocess() in canonical.go; the WHATWG parser needs a scheme. */
function preprocess(raw: string): string {
  const hash = raw.indexOf('#');
  if (hash >= 0) raw = raw.slice(0, hash); // drop the fragment
  raw = escapeLonePercent(
    raw
      .trim()
      .replace(/[\t\r\n]/g, '')
      .replace(/\\/g, '/'), // special schemes treat \ as /
  );
  return SCHEME_RE.test(raw) ? raw : 'http://' + raw; // feeds ship bare "evil.com/x"
}

/**
 * Replace a '%' that does not start a valid escape with "%25". The WHATWG
 * parser does this anyway; Go needs it explicitly, so both do it here.
 */
function escapeLonePercent(s: string): string {
  let out = '';
  for (let i = 0; i < s.length; i++) {
    if (s[i] === '%' && !(i + 2 < s.length && isHexChar(s[i + 1]) && isHexChar(s[i + 2]))) {
      out += '%25';
      continue;
    }
    out += s[i];
  }
  return out;
}

function isHexChar(c: string): boolean {
  return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
}

/** WHATWG has already lowercased the host and applied IDNA (Go uses x/net/idna). */
function canonHost(h: string): string {
  h = unescape(h).replace(/[.]+/g, '.').replace(/^\.+|\.+$/g, '').toLowerCase();

  if (parseIPAddress(h) !== '') throw new InvalidUrlError('ip_not_allowed');
  if (!HOST_RE.test(h)) throw new InvalidUrlError('invalid_host');
  return h;
}

/**
 * Decode the path, resolve "." / ".." / empty segments, then escape once.
 * The WHATWG parser resolves dot segments too, but only the ones visible before
 * decoding and without collapsing "//", so this runs regardless.
 */
function canonPath(p: string): string {
  p = recursiveUnescape(p);
  if (!p.startsWith('/')) p = '/' + p;

  const segs = p.slice(1).split('/');
  const out: string[] = [];
  for (const s of segs) {
    if (s === '' || s === '.') continue;
    if (s === '..') {
      out.pop();
      continue;
    }
    out.push(s);
  }

  let res = '/' + out.join('/');
  const last = segs[segs.length - 1];
  if (out.length > 0 && (last === '' || last === '.' || last === '..')) res += '/';
  return escapePath(res);
}
