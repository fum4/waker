// waker's wait script, served at /__waker/wait.js and loaded by every starting page (the
// built-in one, and an owner's own, where waker injects the tag). Every 3 s it polls
// /__waker/status: on "up" it reloads into the app. On "failed" or "busy" it stops: a page
// that carries that end's copy (a <template class="waker-end" data-page=…>, server-rendered
// and escaped like the rest of the page: the built-in page) swaps its title and line in
// place; any other page reloads once, so waker answers with that state's own page.
(() => {
  const POLL_MS = 3000;
  const ends = {};
  for (const t of document.querySelectorAll("template.waker-end")) ends[t.dataset.page] = t;
  const poll = setInterval(async () => {
    try {
      const s = await (await fetch("/__waker/status", { cache: "no-store" })).json();
      if (s.state === "up") return location.reload();
      if (s.state !== "failed" && s.state !== "busy") return;
      clearInterval(poll);
      const end = ends[s.state];
      if (!end) return location.reload();
      document.body.dataset.page = s.state;
      document.title = document.getElementById("title").textContent = end.dataset.title;
      document.getElementById("text").replaceChildren(end.content.cloneNode(true));
    } catch (e) {}
  }, POLL_MS);
})();
