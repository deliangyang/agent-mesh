import { meshServiceWorkerURL } from "./swCache";

/** Register the static-asset service worker in production builds only. */
export function registerMeshServiceWorker(): void {
  if (!import.meta.env.PROD) return;
  if (typeof navigator === "undefined" || !("serviceWorker" in navigator)) return;
  const sw = navigator.serviceWorker;
  window.addEventListener("load", () => {
    sw.register(meshServiceWorkerURL()).catch(() => {
      /* ignore: private mode / non-secure contexts */
    });
  });
}
