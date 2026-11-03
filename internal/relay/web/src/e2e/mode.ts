import { reactive } from "vue";

export const meshMode = reactive({
  ready: false,
  local: true, // default local until probed (desktop-safe)
  e2e: false,
});

let probe: Promise<void> | null = null;

/** Probe once: local node UI stays on plaintext OpenAI; relay forces E2E. */
export function ensureMeshMode(): Promise<void> {
  if (probe) return probe;
  probe = (async () => {
    try {
      const resp = await fetch("/v1/openai/mesh", { credentials: "same-origin" });
      if (resp.ok) {
        const body = (await resp.json()) as { local?: boolean };
        meshMode.local = Boolean(body.local);
      } else if (resp.status === 401) {
        meshMode.local = false; // relay gate
      } else {
        meshMode.local = true;
      }
    } catch {
      meshMode.local = true;
    }
    meshMode.e2e = !meshMode.local;
    meshMode.ready = true;
  })();
  return probe;
}

export function isE2EMode(): boolean {
  return meshMode.e2e;
}
