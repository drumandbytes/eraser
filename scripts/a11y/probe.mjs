// Keyboard and structure probe that axe can't do: tab stops, focus rings,
// skip link, mouse-only controls, 320px reflow. Informational, not run in CI.
// usage: node probe.mjs <chrome> <url>...
import puppeteer from "puppeteer-core";

const [chrome, ...urls] = process.argv.slice(2);
const b = await puppeteer.launch({ executablePath: chrome, headless: true, args: ["--no-sandbox"] });
for (const url of urls) {
  const p = await b.newPage();
  await p.setViewport({ width: 1280, height: 900 });
  await p.emulateMediaFeatures([{ name: "prefers-reduced-motion", value: "reduce" }]);
  await p.goto(url, { waitUntil: "networkidle0" });
  const st = await p.evaluate(() => {
    const q = (s) => [...document.querySelectorAll(s)];
    const interactive = "a[href],button,input,select,textarea,summary,[tabindex]";
    return {
      h1: q("h1").length, main: q("main").length,
      skipLink: q("a[href^='#']").some((a) => /skip/i.test(a.textContent)),
      mouseOnly: q("[onclick],[hx-get],[hx-post]").filter((e) => !e.matches(interactive + ",form") && !e.closest("a,button")).length,
      thNoScope: q("th:not([scope])").length,
      smallTargets: q(interactive).filter((e) => { const r = e.getBoundingClientRect(); return r.width && (r.width < 24 || r.height < 24) && getComputedStyle(e).display !== "inline"; }).map((e) => (e.textContent || e.name || e.id).trim().slice(0, 25)),
    };
  });
  const noRing = [];
  let stops = 0;
  for (let i = 0; i < 100; i++) {
    await p.keyboard.press("Tab");
    const f = await p.evaluate(() => {
      const e = document.activeElement;
      if (!e || e === document.body) return null;
      const c = getComputedStyle(e);
      return { id: e.tagName + ":" + (e.textContent || e.name || "").trim().slice(0, 25), ring: (c.outlineStyle !== "none" && parseFloat(c.outlineWidth) > 0) || c.boxShadow !== "none" };
    });
    if (!f) break;
    stops++;
    if (!f.ring) noRing.push(f.id);
  }
  await p.setViewport({ width: 320, height: 800 });
  const overflow = await p.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  console.log(url, JSON.stringify({ ...st, stops, noRing, overflowAt320: overflow }));
  await p.close();
}
await b.close();
