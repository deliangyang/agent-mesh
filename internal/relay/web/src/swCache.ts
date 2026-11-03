/**
 * Bump MESH_SW_CACHE_VERSION when shipping UI changes that must drop stale
 * hashed `/assets/*` entries. Keep `public/sw.js` CACHE in sync (tests enforce).
 */
export const MESH_SW_CACHE_VERSION = 2;

export function meshStaticCacheName(version: number = MESH_SW_CACHE_VERSION): string {
  return `mesh-static-v${version}`;
}

/** Registration URL with a version query so browsers fetch a fresh worker script. */
export function meshServiceWorkerURL(version: number = MESH_SW_CACHE_VERSION): string {
  return `/sw.js?v=${version}`;
}
