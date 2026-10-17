// Runs axe-core (WCAG 2.x A/AA) over the given URLs in light and dark mode.
// usage: node scan.mjs <chrome> <url>...   exit 1 on any violation
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import puppeteer from "puppeteer-core";

const require = createRequire(import.meta.url);
const axe = readFileSync(require.resolve("axe-core/axe.min.js"), "utf8");
const [chrome, ...urls] = process.argv.slice(2);
const browser = await puppeteer.launch({ executablePath: chrome, headless: true, args: ["--no-sandbox"] });
let failed = 0;
for (const url of urls) {
  for (const scheme of ["light", "dark"]) {
    const page = await browser.newPage();
    await page.setBypassCSP(true);
    await page.emulateMediaFeatures([{ name: "prefers-color-scheme", value: scheme }, { name: "prefers-reduced-motion", value: "reduce" }]);
    await page.evaluateOnNewDocument((s) => { try { localStorage.setItem("eraser-theme", s); } catch {} }, scheme);
    await page.goto(url, { waitUntil: "networkidle0" });
    await new Promise((r) => setTimeout(r, 1000)); // let entrance animations settle, axe reads mid-fade colours
    await page.evaluate(axe);
    const { violations } = await page.evaluate(() =>
      axe.run({ runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"] } }));
    for (const v of violations) {
      failed++;
      console.log(`${url} [${scheme}] ${v.id} (${v.impact}): ${v.help}`);
      for (const n of v.nodes.slice(0, 3)) console.log(`    ${n.target.join(" ")}\n    ${n.failureSummary.split("\n").slice(0, 2).join(" | ")}`);
    }
    await page.close();
  }
}
await browser.close();
console.log(failed ? `${failed} violation(s)` : "no violations");
process.exit(failed ? 1 : 0);
