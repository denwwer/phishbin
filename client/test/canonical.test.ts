import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

import { candidates, InvalidUrlError, parse, pattern } from '../src/canonical';
import { canonicalNum, parseIPAddress, recursiveUnescape } from '../src/sb_urls';

// The contract with internal/normalize (Go). Both sides must pass it.
const fixtures: {
  canonical: { in: string; out: string | null }[];
  candidates: { in: string; out: string[] }[];
} = JSON.parse(readFileSync(new URL('../../testdata/normalize.json', import.meta.url), 'utf8'));

describe('canonical', () => {
  for (const c of fixtures.canonical) {
    it(`${JSON.stringify(c.in)} -> ${c.out === null ? 'rejected' : c.out}`, () => {
      if (c.out === null) {
        expect(() => parse(c.in)).toThrow(InvalidUrlError);
      } else {
        expect(pattern(parse(c.in))).toBe(c.out);
      }
    });
  }
});

describe('candidates', () => {
  for (const c of fixtures.candidates) {
    it(c.in, () => {
      expect(candidates(parse(c.in)).sort()).toEqual(c.out);
    });
  }

  it('always contains the URL own stored pattern', () => {
    for (const c of fixtures.canonical) {
      if (c.out === null) continue;
      expect(candidates(parse(c.in))).toContain(c.out);
    }
  });
});

describe('helpers', () => {
  it('recursiveUnescape decodes until stable', () => {
    expect(recursiveUnescape('%252525252525252541')).toBe('A');
  });

  it('parseIPAddress handles legacy encodings', () => {
    expect(parseIPAddress('1.2.3.4')).toBe('1.2.3.4');
    expect(parseIPAddress('0x7f000001')).toBe('127.0.0.1');
    expect(parseIPAddress('127.1')).toBe('127.0.0.1');
    expect(parseIPAddress('012.034.01.055')).toBe('10.28.1.45');
    expect(parseIPAddress('10.192.95.89 xy')).toBe('10.192.95.89');
    expect(parseIPAddress('evil.com')).toBe('');
    expect(parseIPAddress('1.2.3.4.5')).toBe('');
  });

  it('canonicalNum matches Go strconv.ParseUint base 0', () => {
    expect(canonicalNum('0x10203040', 4)).toBe('16.32.48.64');
    expect(canonicalNum('01234', 2)).toBe('2.156');
    expect(canonicalNum('08', 1)).toBe(''); // invalid octal, like Go
    expect(canonicalNum('4294967296', 4)).toBe(''); // > 32 bits
  });
});
