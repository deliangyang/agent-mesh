/** Paths that must never be cached by the Mesh service worker. */
export function isMeshAPIRequest(pathname: string): boolean {
  const p = pathname.split("?")[0] || "/";
  if (p === "/v1" || p.startsWith("/v1/")) return true;
  if (p === "/login" || p === "/logout") return true;
  if (p === "/api" || p.startsWith("/api/")) return true;
  return false;
}

/** Same-origin static shell/assets safe to precache or runtime-cache. */
export function isMeshStaticAsset(pathname: string): boolean {
  const p = pathname.split("?")[0] || "/";
  if (isMeshAPIRequest(p)) return false;
  if (p === "/" || p === "/index.html") return true;
  if (p === "/manifest.webmanifest" || p === "/favicon.svg" || p === "/sw.js") return true;
  if (p.startsWith("/assets/")) return true;
  return /\.(?:js|css|svg|png|ico|webmanifest|woff2?)$/i.test(p);
}
