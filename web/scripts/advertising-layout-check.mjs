// Read-only check of an already open advertising project; no clicks or business writes.
// bun scripts/advertising-layout-check.mjs http://127.0.0.1:64266
import assert from "node:assert/strict";
const tabs = await (await fetch((process.argv[2] || "http://127.0.0.1:64266") + "/json/list")).json();
const tab = tabs.find((t) => t.type === "page" && t.url.includes("/ad-projects/"));
assert.ok(tab, "Open an advertising project first");
const ws = new WebSocket(tab.webSocketDebuggerUrl);
await new Promise((resolve, reject) => {
    ws.onopen = resolve;
    ws.onerror = reject;
});
let id = 0;
const pending = new Map();
ws.onmessage = (e) => {
    const m = JSON.parse(e.data);
    if (m.id) {
        pending.get(m.id)(m);
        pending.delete(m.id);
    }
};
const send = (method, params = {}) =>
    new Promise((resolve) => {
        pending.set(++id, resolve);
        ws.send(JSON.stringify({ id, method, params }));
    });
try {
    for (const [width, height] of [
        [1200, 737],
        [390, 844],
    ]) {
        await send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false });
        await send("Runtime.evaluate", { expression: "new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))", awaitPromise: true });
        const result = await send("Runtime.evaluate", {
            returnByValue: true,
            expression: `(() => {
        const box = selector => document.querySelector(selector).getBoundingClientRect();
        const footer = box(".ad-action-bar"), content = box(".ad-workbench-grid");
        const primary = document.querySelector(".ad-action-bar .ad-actions > div button");
        const rect = primary?.getBoundingClientRect();
        const clear = rect && [0.1, 0.5, 0.9].every(x => [0.1, 0.5, 0.9].every(y => {
            const hit = document.elementFromPoint(rect.x + rect.width * x, rect.y + rect.height * y);
            return hit === primary || primary.contains(hit);
        }));
        return { bottomGap: innerHeight - footer.bottom, overlap: content.bottom - footer.top,
            scrollable: ["auto", "scroll"].includes(getComputedStyle(document.querySelector(".ad-workbench-grid")).overflowY),
            primaryClear: !!clear,
            overflow: document.documentElement.scrollWidth > innerWidth,
            assetWidth: document.querySelector(".ad-brief-grid > div > .ad-section")?.getBoundingClientRect().width };
    })()`,
        });
        assert.ok(!result.exceptionDetails, JSON.stringify(result.exceptionDetails));
        const value = result.result.result.value;
        console.log(value);
        assert.ok(value.bottomGap <= 24, "Footer floats above viewport bottom");
        assert.ok(value.overlap <= 1, "Footer overlays the content viewport");
        assert.ok(value.scrollable, "Content needs its own bounded scroll area");
        assert.ok(value.primaryClear, "Primary action is obscured");
        assert.equal(value.overflow, false);
        assert.ok(value.assetWidth === undefined || value.assetWidth >= 300, "Asset column is squeezed");
        console.log("PASS:", width, "bounded content, docked footer, clear primary, readable asset column");
    }
} finally {
    await send("Emulation.clearDeviceMetricsOverride");
    ws.close();
}
