// waker's wait script, served at /__waker/wait.js and loaded by every starting page (the
// built-in one, and an owner's own, where waker injects the tag). Every 3 s it polls
// /__waker/status: on "up" it reloads into the app. On "failed" or "busy" it stops: a page
// that carries the end copy (#waker-ends, the built-in page) swaps its title and text in
// place; any other page reloads once, so waker answers with that state's own page.
(() => {
  const POLL_MS = 3000;
  const el = document.getElementById("waker-ends");
  const ends = el ? JSON.parse(el.textContent) : null;
  const poll = setInterval(async () => {
    try {
      const s = await (await fetch("/__waker/status", { cache: "no-store" })).json();
      if (s.state === "up") return location.reload();
      if (s.state !== "failed" && s.state !== "busy") return;
      clearInterval(poll);
      if (!ends || !ends[s.state]) return location.reload();
      document.body.dataset.page = s.state;
      document.title = document.getElementById("title").textContent = ends[s.state][0];
      document.getElementById("text").textContent = ends[s.state][1];
    } catch (e) {}
  }, POLL_MS);
})();
