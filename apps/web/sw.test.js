import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { runInNewContext } from 'node:vm';

test('el service worker solo intercepta assets versionados del mismo origen', () => {
  const listeners = new Map();
  const cache = { match: async () => 'asset en caché' };
  runInNewContext(readFileSync(new URL('./public/sw.js', import.meta.url), 'utf8'), {
    self: {
      location: { origin: 'https://retail.example' },
      addEventListener: (name, handler) => listeners.set(name, handler),
    },
    caches: { open: async () => cache },
    URL,
  });

  function intercept(url, method = 'GET') {
    let result;
    listeners.get('fetch')({
      request: { url: new URL(url, 'https://retail.example').href, method },
      respondWith: (promise) => { result = promise; },
    });
    return result;
  }

  assert.equal(intercept('/api/v1/me'), undefined);
  assert.equal(intercept('/api/v1/auth/logout', 'POST'), undefined);
  assert.equal(intercept('/'), undefined);
  assert.equal(intercept('https://external.example/assets/index-abc123.js'), undefined);
  return assert.doesNotReject(async () => {
    assert.equal(await intercept('/assets/index-abc123.js'), 'asset en caché');
  });
});
