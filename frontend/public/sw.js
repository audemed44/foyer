// Foyer's service worker: makes the dashboard installable and keeps the app
// shell available when the server can't be reached. The API is never cached.
const SHELL = "foyer-shell-v1";
const ASSETS = "foyer-assets-v1";

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(SHELL).then((c) => c.addAll(["/", "/favicon.svg", "/icon-192.png"])),
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(keys.filter((k) => k !== SHELL && k !== ASSETS).map((k) => caches.delete(k))),
      )
      .then(() => self.clients.claim()),
  );
});

// The main script's hashed name changes with each release; when it does, the
// old build's assets are dropped.
const entry = (html) => html.match(/\/assets\/index-[\w-]+\.js/)?.[0] ?? "";

async function navigate(request) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      const shell = await caches.open(SHELL);
      const cached = await shell.match("/");
      const html = await response.clone().text();
      if (cached && entry(await cached.text()) !== entry(html)) await caches.delete(ASSETS);
      await shell.put("/", response.clone());
    }
    return response;
  } catch {
    return (await caches.match("/")) ?? Response.error();
  }
}

async function asset(request) {
  const cached = await caches.match(request);
  if (cached) return cached;
  const response = await fetch(request);
  if (response.ok) (await caches.open(ASSETS)).put(request, response.clone());
  return response;
}

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;
  if (request.mode === "navigate") event.respondWith(navigate(request));
  else if (url.pathname.startsWith("/assets/")) event.respondWith(asset(request));
});
