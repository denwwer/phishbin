// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// TypeScript port of the helpers in github.com/google/safebrowsing/urls.go
// (Apache-2.0). Line-for-line mirror of internal/normalize/sb_urls.go — see the
// MODIFICATIONS note there. Change both files together or lookups stop matching.

/** Percent-encode. `extra` holds characters escaped beyond Google's set. */
export function escape(s: string, extra: string): string {
  let out = '';
  for (const c of bytesOf(s)) {
    if (c < 0x20 || c >= 0x7f || c === 0x20 || c === 0x23 || c === 0x25 || extra.includes(String.fromCharCode(c))) {
      out += '%' + c.toString(16).padStart(2, '0'); // lowercase, like Go's %02x
    } else {
      out += String.fromCharCode(c);
    }
  }
  return out;
}

export const escapePath = (s: string) => escape(s, '?');
export const escapeQuery = (s: string) => escape(s, '');

/** Decode one percent-encoding pass. Operates on bytes, like the Go version. */
export function unescape(s: string): string {
  let out = '';
  for (let i = 0; i < s.length; i++) {
    if (i + 2 < s.length && s[i] === '%' && isHex(s[i + 1]) && isHex(s[i + 2])) {
      out += String.fromCharCode((unhex(s[i + 1]) << 4) | unhex(s[i + 2]));
      i += 2;
    } else {
      out += s[i];
    }
  }
  return out;
}

/** Decode until stable, so %2520 and friends cannot hide a separator. */
export function recursiveUnescape(s: string): string {
  const maxDepth = 1024;
  for (let i = 0; i < maxDepth; i++) {
    const t = unescape(s);
    if (t === s) return s;
    s = t;
  }
  throw new Error('normalize: unescaping is too recursive');
}

const possibleIPRe = /^(?:0x[0-9a-f]+|[0-9.])+$/i;
const trailingSpaceRe = /^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}) /;

/**
 * Canonical dotted-decimal form of an IP host, or '' if it is not one.
 * Handles the hex, octal and short forms legacy resolvers accept
 * (0x7f000001, 127.1, "10.192.95.89 xy").
 */
export function parseIPAddress(iphostname: string): string {
  if (iphostname.length <= 15) {
    const m = trailingSpaceRe.exec(iphostname);
    if (m) iphostname = m[0].trim();
  }
  if (!possibleIPRe.test(iphostname)) return '';

  const parts = iphostname.split('.');
  if (parts.length > 4) return '';

  const ss: string[] = [];
  for (let i = 0; i < parts.length; i++) {
    const n = i === parts.length - 1 ? 5 - parts.length : 1;
    const s = canonicalNum(parts[i], n);
    if (s === '') return '';
    ss.push(s);
  }
  return ss.join('.');
}

/** "0x10203040", 4 => "16.32.48.64";  "01234", 2 => "2.156" */
export function canonicalNum(s: string, n: number): string {
  if (n <= 0 || n > 4) return '';
  const v = parseUintBase0(s);
  if (v === null) return '';

  const ss = new Array<string>(n);
  let rest = v;
  for (let i = n - 1; i >= 0; i--) {
    ss[i] = String(rest % 256);
    rest = Math.floor(rest / 256);
  }
  return ss.join('.');
}

/** Go's strconv.ParseUint(s, 0, 32): 0x hex, leading-0 octal, else decimal. */
function parseUintBase0(s: string): number | null {
  let v: number;
  if (/^0[xX][0-9a-fA-F]+$/.test(s)) v = parseInt(s.slice(2), 16);
  else if (/^0[0-7]*$/.test(s)) v = parseInt(s, 8);
  else if (/^[1-9][0-9]*$/.test(s)) v = parseInt(s, 10);
  else return null;
  return v > 0xffffffff ? null : v;
}

/** Host plus up to 4 parent suffixes (never the bare TLD). */
export function generateLookupHosts(host: string, isIP: boolean): string[] {
  const maxHostComponents = 7;
  if (isIP) return [host];

  const c = host.split('.');
  const start = Math.max(1, c.length - maxHostComponents);

  const hosts = [host];
  for (let i = start; i < c.length - 1; i++) hosts.push(c.slice(i).join('.'));
  return hosts;
}

/** '/', then up to 3 parent directories, then the full path and path+query. */
export function generateLookupPaths(path: string, query: string): string[] {
  const maxPathComponents = 4;

  const paths = ['/'];
  const comps = path.split('/').filter((p) => p !== '');
  const num = Math.min(comps.length, maxPathComponents);
  for (let i = 1; i < num; i++) paths.push('/' + comps.slice(0, i).join('/') + '/');
  if (path !== '/') paths.push(path);
  if (query.length > 0) paths.push(path + '?' + query);
  return paths;
}

// --- helpers ---

const utf8 = new TextEncoder();

/** Byte values of s. Chars above 0xFF are UTF-8 encoded, as Go strings are. */
function bytesOf(s: string): number[] {
  const out: number[] = [];
  for (const ch of s) {
    const c = ch.codePointAt(0)!;
    if (c <= 0xff) out.push(c);
    else for (const b of utf8.encode(ch)) out.push(b);
  }
  return out;
}

function isHex(c: string): boolean {
  return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
}

function unhex(c: string): number {
  if (c >= '0' && c <= '9') return c.charCodeAt(0) - 48;
  if (c >= 'a' && c <= 'f') return c.charCodeAt(0) - 87;
  if (c >= 'A' && c <= 'F') return c.charCodeAt(0) - 55;
  return 0;
}
