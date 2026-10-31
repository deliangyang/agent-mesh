import { createApp } from "vue";
import App from "./App.vue";
import { router } from "./router";
import { applyTheme, loadTheme } from "./theme";
import "@copilotkit/vue/styles.css";
import "./style.css";

function lockPageZoom(): void {
  const opts: AddEventListenerOptions = { capture: true, passive: false };
  const block = (event: Event) => event.preventDefault();
  for (const type of ["gesturestart", "gesturechange", "gestureend"] as const) {
    document.addEventListener(type, block, opts);
    window.addEventListener(type, block, opts);
  }
  const pinch = (event: TouchEvent) => {
    if (event.touches.length > 1) event.preventDefault();
    const scaled = event as TouchEvent & { scale?: number };
    if (typeof scaled.scale === "number" && scaled.scale !== 1) event.preventDefault();
  };
  document.addEventListener("touchstart", pinch, opts);
  document.addEventListener("touchmove", pinch, opts);
  window.addEventListener("touchstart", pinch, opts);
  window.addEventListener("touchmove", pinch, opts);
  document.addEventListener(
    "wheel",
    (event) => {
      if (event.ctrlKey || event.metaKey) event.preventDefault();
    },
    opts,
  );
}

lockPageZoom();
applyTheme(loadTheme());
createApp(App).use(router).mount("#app");
