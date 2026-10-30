// waker's heartbeat, served at /__waker/heartbeat.js and injected into every HTML page of an
// app (opt out per app: `heartbeat: false`). At most once a minute, while the tab is visible
// and the visitor gave input within the last minute: POST /__waker/active, so an on-demand
// app isn't stopped under a reader.
(() => {
  const MINUTE_MS = 60000;
  let lastInput = Date.now();
  const onInput = () => { lastInput = Date.now(); };
  for (const type of ["pointerdown", "pointermove", "keydown", "scroll", "touchstart", "wheel"]) {
    addEventListener(type, onInput, { capture: true, passive: true });
  }
  setInterval(() => {
    if (document.visibilityState === "visible" && Date.now() - lastInput <= MINUTE_MS) {
      navigator.sendBeacon("/__waker/active");
    }
  }, MINUTE_MS);
})();
