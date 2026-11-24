const cacheName = 'retail-assets-v1';

self.addEventListener('activate', (event) => {
  event.waitUntil(caches.keys().then((keys) => Promise.all(
    keys.filter((key) => key.startsWith('retail-assets-') && key !== cacheName)
      .map((key) => caches.delete(key)),
  )));
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  if (event.request.method !== 'GET' || url.origin !== self.location.origin ||
      !/^\/assets\/[\w-]+-[\w-]+\.(js|css)$/.test(url.pathname)) {
    return;
  }
  event.respondWith((async () => {
    const cache = await caches.open(cacheName).catch(() => null);
    const cached = await cache?.match(event.request).catch(() => undefined);
    if (cached) return cached;
    const response = await fetch(event.request);
    if (response.ok && cache) {
      await cache.put(event.request, response.clone()).catch(() => undefined);
    }
    return response;
  })());
});
