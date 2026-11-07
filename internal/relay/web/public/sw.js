/* Agent Mesh offline shell: cache static assets only. Never cache /v1, /login, /logout.
 * Bump CACHE (and src/swCache.ts MESH_SW_CACHE_VERSION) when hashed assets must invalidate. */
const CACHE = "mesh-static-v5";
const PRECACHE = ["/", "/index.html", "/manifest.webmanifest", "/favicon.svg"];

function isAPI(pathname) {
  const p = pathname.split("?")[0] || "/";
  if (p === "/v1" || p.startsWith("/v1/")) return true;
  if (p === "/login" || p === "/logout") return true;
  if (p === "/api" || p.startsWith("/api/")) return true;
  return false;
}

function isStatic(pathname) {
  const p = pathname.split("?")[0] || "/";
  if (isAPI(p)) return false;
  if (p === "/" || p === "/index.html") return true;
  if (p === "/manifest.webmanifest" || p === "/favicon.svg" || p === "/sw.js") return true;
  if (p.startsWith("/assets/")) return true;
  return /\.(?:js|css|svg|png|ico|webmanifest|woff2?)$/i.test(p);
}

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE)
      .then((cache) => cache.addAll(PRECACHE))
      .then(() => self.skipWaiting())
      .catch(() => self.skipWaiting()),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;
  let url;
  try {
    url = new URL(req.url);
  } catch {
    return;
  }
  if (url.origin !== self.location.origin) return;
  const path = url.pathname;
  if (isAPI(path)) return;

  if (req.mode === "navigate") {
    event.respondWith(
      fetch(req).catch(() =>
        caches.match("/index.html").then((hit) => hit || caches.match("/") || Response.error()),
      ),
    );
    return;
  }

  if (!isStatic(path)) return;

  event.respondWith(
    caches.open(CACHE).then(async (cache) => {
      const cached = await cache.match(req);
      try {
        const res = await fetch(req);
        if (res && res.ok) {
          cache.put(req, res.clone());
        }
        return res;
      } catch (err) {
        if (cached) return cached;
        throw err;
      }
    }),
  );
});
