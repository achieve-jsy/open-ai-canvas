/** Real built UI + native-schema SQLite + real worker/FFmpeg. TEST media only; no paid calls.
 * Run: bun run build && bun scripts/advertising-generation-chrome-e2e.mjs
 * Go fixture routes/provider exist only in *_test.go. Only owned children/temp dirs are cleaned.
 */
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, existsSync, rmSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { fileURLToPath } from "node:url";
const root = resolve(fileURLToPath(new URL("../..", import.meta.url)));
const dir = mkdtempSync(join(tmpdir(), "yingce-ad-browser-"));
const artifacts = join(root, ".superpowers/sdd/2026-11-04-advertising-generation/browser");
mkdirSync(artifacts, { recursive: true });
const children = [];
let socket;
let backend;
let backendLog = "";
let browserLog = "";
let inspect;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function until(fn, label, timeout = 30000) {
    const end = Date.now() + timeout;
    while (Date.now() < end) {
        const v = await fn();
        if (v) return v;
        await sleep(200);
    }
    throw Error("Timeout: " + label);
}
function start(command, args, options) {
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "pipe"], ...options });
    children.push(child);
    return child;
}
try {
    backend = start("go", ["test", "-v", "./internal/handler", "-run", "^TestAdvertisingBrowserServer$", "-count=1", "-timeout", "9m"], { cwd: join(root, "backend"), env: { ...process.env, ADVERTISING_BROWSER_TEST_DIR: dir } });
    for (const out of [backend.stdout, backend.stderr]) out.on("data", (b) => (backendLog += b));
    await until(
        () => {
            if (backend.exitCode !== null) throw Error(backendLog);
            return existsSync(join(dir, "server-url"));
        },
        "test backend",
        60000,
    );
    const origin = readFileSync(join(dir, "server-url"), "utf8");
    console.log("TEST backend " + origin);
    const chrome = process.env.CHROME_PATH || ["/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/usr/bin/google-chrome", "/usr/bin/chromium"].find(existsSync);
    assert(chrome, "Chrome required");
    const profile = join(dir, "chrome");
    const browser = start(chrome, ["--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--disable-dev-shm-usage", "--remote-debugging-port=0", "--user-data-dir=" + profile, "about:blank"], {});
    for (const out of [browser.stdout, browser.stderr]) out.on("data", (b) => (browserLog += b));
    await until(() => existsSync(join(profile, "DevToolsActivePort")), "Chrome CDP");
    const port = readFileSync(join(profile, "DevToolsActivePort"), "utf8").split("\n")[0];
    const targets = await (await fetch("http://127.0.0.1:" + port + "/json/list")).json();
    socket = new WebSocket(targets.find((t) => t.type === "page").webSocketDebuggerUrl);
    await new Promise((res, rej) => {
        socket.addEventListener("open", res, { once: true });
        socket.addEventListener("error", rej, { once: true });
    });
    let sequence = 0;
    const pending = new Map();
    const problems = [];
    socket.addEventListener("message", (event) => {
        const msg = JSON.parse(event.data);
        if (msg.id) {
            const p = pending.get(msg.id);
            if (p) {
                pending.delete(msg.id);
                clearTimeout(p.timer);
                msg.error ? p.reject(Error(JSON.stringify(msg.error))) : p.resolve(msg.result);
            }
        } else if (msg.method === "Network.responseReceived" && msg.params.response.url.includes("/api/")) {
            const { status, url } = msg.params.response;
            console.log(status, url.split("/api/")[1]);
            if (url.includes("/api/dramas/ad_")) problems.push("Advertising canvas requested drama: " + status);
        } else if (msg.method === "Runtime.exceptionThrown") problems.push(msg.params.exceptionDetails.exception?.description || msg.params.exceptionDetails.text);
    });
    const send = (method, params = {}) =>
        new Promise((resolve, reject) => {
            const id = ++sequence;
            const timer = setTimeout(() => {
                pending.delete(id);
                reject(Error("CDP timeout " + method));
            }, 30000);
            pending.set(id, { resolve, reject, timer });
            socket.send(JSON.stringify({ id, method, params }));
        });
    const evaluate = async (expression) => {
        const r = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true, userGesture: true });
        if (r.exceptionDetails) throw Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text);
        return r.result.value;
    };
    inspect = () => evaluate("JSON.stringify({url:location.href,text:document.body.innerText,inputs:Array.from(document.querySelectorAll('input')).map(e=>({type:e.type,valid:e.validity.valid,message:e.validationMessage}))})");
    const click = async (text, scope = "document") => {
        await until(() => evaluate(`(()=>{const b=Array.from(` + scope + `.querySelectorAll('button')).find(b=>b.textContent.trim()===` + JSON.stringify(text) + `&&!b.disabled);if(!b)return false;b.click();return true})()`), "button " + text);
        await sleep(150);
    };
    const input = async (selector, value) => {
        await until(() => evaluate("!!document.querySelector(" + JSON.stringify(selector) + ")"), selector);
        await evaluate(
            `(()=>{const e=document.querySelector(` +
                JSON.stringify(selector) +
                `);Object.getOwnPropertyDescriptor(e.tagName==='SELECT'?HTMLSelectElement.prototype:HTMLInputElement.prototype,'value').set.call(e,` +
                JSON.stringify(value) +
                `);e.dispatchEvent(new Event(e.tagName==='SELECT'?'change':'input',{bubbles:true}));})()`,
        );
        await sleep(120);
    };
    const api = (path, method = "GET", body) =>
        evaluate(
            `(async()=>{const r=await fetch('/api'+` +
                JSON.stringify(path) +
                `,{method:` +
                JSON.stringify(method) +
                `,headers:{'Content-Type':'application/json'},body:` +
                (body === undefined ? "undefined" : JSON.stringify(JSON.stringify(body))) +
                `});const e=await r.json();if(!r.ok||e.code!==0)throw Error(r.status+': '+e.msg);return e.data;})()`,
        );
    await send("Runtime.enable");
    await send("Page.enable");
    await send("Network.enable");
    await send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    await send("Page.navigate", { url: origin + "/register" });
    await input('input[autocomplete="username"]', "test_ad_browser");
    await input('input[placeholder="至少 8 位"]', "Test-only-38!pass");
    await input('input[placeholder="再次输入密码"]', "Test-only-38!pass");
    await until(() => evaluate("!!document.querySelector('input[type=checkbox]')"), "terms");
    await evaluate("document.querySelector('input[type=checkbox]').click()");
    await click("创建账号");
    await sleep(1000);
    console.log("Submitted registration");
    await until(() => evaluate("!location.pathname.includes('register')&&!location.pathname.includes('login')"), "registered session");
    await api("/test-fixture", "POST", {});
    const shots = Array.from({ length: 3 }, (_, i) => ({ id: crypto.randomUUID(), durationMs: 5000, visual: "TEST shot " + (i + 1), voiceover: i === 0 ? "TEST voice" : "", onScreenText: "TEST " + (i + 1), referenceResourceIds: [] }));
    const draft = {
        schemaVersion: 1,
        brief: { productName: "TEST PRODUCT", description: "Synthetic test pattern, not a commercial", audience: "TEST", sellingPoints: ["TEST"], cta: "TEST", durationMs: 15000 },
        brand: { name: "TEST", primaryColor: "#ffffff", logoResourceId: "test-product" },
        productResourceIds: ["test-product"],
        additionalResourceIds: ["test-music"],
        shots,
    };
    const ad = await api("/ad-projects", "POST", { name: "TEST browser export", draft });
    await send("Page.navigate", { url: origin + "/ad-projects/" + ad.project.id });
    await click("确认脚本");
    await click("确认并冻结");
    await click("进入此版本制作");
    const base = "/ad-projects/" + ad.project.id;
    const candidateReady = async (scope, label) => {
        const text = await evaluate(scope + ".innerText");
        assert(!text.includes("任务失败"), text);
        return evaluate(`Array.from(` + scope + `.querySelectorAll('button')).some(b=>b.textContent.trim()===` + JSON.stringify(label) + `&&!b.disabled)`);
    };
    for (let i = 0; i < 3; i++) {
        const scope = `document.querySelector('[aria-label="镜头` + (i + 1) + `制作"]')`;
        await input('[aria-label="镜头' + (i + 1) + '视频模型"]', "test-video");
        await click("获取视频报价", scope);
        let evidence = await api("/test-evidence");
        assert.equal(evidence.videoCalls, i, "quote must not call provider");
        await click("查看并确认报价", scope);
        await click("确认费用并生成");
        await until(() => candidateReady(scope, "采用此视频"), "video candidate " + i, 90000);
        await evaluate(scope + ".querySelector('video').scrollIntoView({block:'center'})");
        await evaluate(scope + ".querySelector('video').play()");
        await sleep(350);
        await until(() => evaluate(scope + ".querySelector('video').currentTime > 0"), "video playback", 10000);
        await evaluate(scope + ".querySelector('video').pause()");
        await click("采用此视频", scope);
    }
    const scope = `document.querySelector('[aria-label="镜头1制作"]')`;
    await input('[aria-label="镜头1制作"] fieldset:nth-of-type(2) select', "test-audio");
    await input('[aria-label="镜头1制作"] fieldset:nth-of-type(2) label:nth-of-type(2) select', "alloy");
    await click("获取旁白报价", scope);
    assert.equal((await api("/test-evidence")).audioCalls, 0);
    await click("查看并确认报价", scope);
    await click("确认费用并生成");
    await until(() => candidateReady(scope, "采用此旁白"), "TTS candidate", 90000);
    await evaluate(scope + ".querySelector('audio').scrollIntoView({block:'center'})");
    await evaluate(scope + ".querySelector('audio').play()");
    await sleep(350);
    await until(() => evaluate(scope + ".querySelector('audio').currentTime > 0"), "audio playback", 10000);
    await evaluate(scope + ".querySelector('audio').pause()");
    await click("采用此旁白", scope);
    await input(".ad-production > fieldset label:nth-of-type(3) select", "test-music");
    await click("保存制作设置");
    const previousLoad = await evaluate("performance.timeOrigin");
    await send("Page.reload");
    await until(async () => {
        try {
            return (await evaluate("performance.timeOrigin")) !== previousLoad;
        } catch {
            return false;
        }
    }, "new document after reload");
    await click("进入此版本制作");
    await until(() => evaluate("document.querySelectorAll('.ad-candidate').length===4"), "restored candidates");
    const detail = await api(base);
    const production = await api(base + "/productions", "POST", { confirmedRevisionId: detail.latestConfirmedRevisionId });
    const pbase = base + "/productions/" + production.production.id;
    const expiredQuote = await api(pbase + "/submissions", "POST", { clientOperationId: crypto.randomUUID(), shotId: shots[0].id, kind: "video", modelId: "test-video" });
    assert.equal((await api("/test-expire-quote/" + expiredQuote.id, "POST")).expired, true);
    const expiredResponse = await evaluate(
        `(async()=>{const r=await fetch("/api"+` +
            JSON.stringify(pbase + "/submissions/" + expiredQuote.id + "/execute") +
            `,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({quoteHash:` +
            JSON.stringify(expiredQuote.quote.quoteHash) +
            `})});return {status:r.status,body:await r.json()}})()`,
    );
    assert.equal(expiredResponse.status, 409);
    assert.equal(expiredResponse.body.reason, "advertising_quote_expired");
    const subs = await api(pbase + "/submissions");
    const first = subs.items.find((s) => s.kind === "video" && s.task);
    const replay = await api(pbase + "/submissions/" + first.id + "/execute", "POST", { quoteHash: first.quote.quoteHash });
    assert.equal(replay.task.id, first.task.id);
    assert.equal((await api("/test-evidence")).videoCalls, 3);
    await click("组装新时间线");
    await until(() => evaluate("!!Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='导出原组装')"), "assembly", 30000);
    for (const [theme, width] of [
        ["light", 1440],
        ["dark", 1440],
        ["dark", 390],
    ]) {
        await send("Emulation.setDeviceMetricsOverride", { width, height: 1000, deviceScaleFactor: 1, mobile: width === 390 });
        if ((await evaluate("document.documentElement.classList.contains('dark')")) !== (theme === "dark")) await click("切换主题");
        await until(() => evaluate("document.documentElement.classList.contains('dark')").then((dark) => dark === (theme === "dark")), "theme " + theme);
        await evaluate("document.querySelector('.ad-production').scrollIntoView()");
        await sleep(300);
        assert(await evaluate("document.documentElement.scrollWidth <= innerWidth+1"), "horizontal overflow " + width);
        const shot = await send("Page.captureScreenshot", { format: "png" });
        writeFileSync(join(artifacts, theme + "-" + width + ".png"), Buffer.from(shot.data, "base64"));
    }
    await send("Input.dispatchKeyEvent", { type: "keyDown", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 });
    await send("Input.dispatchKeyEvent", { type: "keyUp", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 });
    assert(await evaluate("document.activeElement!==document.body"));
    await click("导出原组装");
    await until(() => evaluate("!!Array.from(document.querySelectorAll('a')).find(a=>a.textContent==='下载成片MP4')"), "MP4 export", 90000);
    const url = await evaluate("Array.from(document.querySelectorAll('a')).find(a=>a.textContent==='下载成片MP4').href");
    const cookies = await send("Network.getCookies", { urls: [origin] });
    const res = await fetch(url, { headers: { Cookie: cookies.cookies.map((c) => c.name + "=" + c.value).join("; ") } });
    assert(res.ok);
    const mp4 = join(artifacts, "TEST-output.mp4");
    writeFileSync(mp4, Buffer.from(await res.arrayBuffer()));
    const probe = spawnSync("ffprobe", ["-v", "error", "-show_streams", "-show_format", "-of", "json", mp4], { encoding: "utf8" });
    assert.equal(probe.status, 0);
    const metadata = JSON.parse(probe.stdout);
    assert(metadata.streams.some((s) => s.codec_type === "audio"));
    assert(metadata.streams.some((s) => s.codec_type === "video" && s.width === 1080 && s.height === 1920));
    assert(Math.abs(Number(metadata.format.duration) - 15) < 0.034);
    const assembly = (await api(pbase + "/assemblies")).items[0];
    const renderPath = pbase + "/assemblies/" + assembly.id + "/renders";
    const cancelKey = crypto.randomUUID();
    const attempt = await api(renderPath, "POST", { clientOperationId: cancelKey });
    const cancelled = await api("/tasks/" + attempt.task.id + "/cancel", "POST");
    assert.equal(cancelled.status, "cancelled");
    const cancelledReplay = await api(renderPath, "POST", { clientOperationId: cancelKey });
    assert.equal(cancelledReplay.task.id, attempt.task.id);
    assert.equal(cancelledReplay.task.status, "cancelled");
    assert.equal(cancelledReplay.available, false);
    const evidence = await api("/test-evidence");
    assert.equal(evidence.videoCalls, 3);
    assert.equal(evidence.audioCalls, 1);
    assert.equal(evidence.tasks.length, 6);
    assert.equal(evidence.account.reservedMicrocredits, 0);
    assert.equal(evidence.account.availableMicrocredits, 9975, "three 5-unit videos and one 10-unit TTS; replay/export do not bill again");
    writeFileSync(join(artifacts, "evidence.json"), JSON.stringify({ videoCalls: evidence.videoCalls, audioCalls: evidence.audioCalls, account: evidence.account, metadata, problems }, null, 2));
    const canvas = await evaluate("Array.from(document.querySelectorAll('a')).find(a=>a.textContent==='打开时间线继续编辑').href");
    await send("Page.navigate", { url: canvas });
    await until(() => evaluate("location.pathname.startsWith('/canvas/') && document.body.innerText.length>50"), "real canvas route");
    await sleep(1500);
    assert.deepEqual(problems, []);
    writeFileSync(join(dir, "stop"), "");
    await until(() => backend.exitCode !== null, "clean backend shutdown", 15000);
    assert.equal(backend.exitCode, 0, backendLog);
    console.log("PASS: real registration/confirmation/quote/approval/video/TTS/playback/adoption/music/refresh/replay/billing/cancelled-render/assembly/MP4/canvas; TEST media only");
} catch (error) {
    if (inspect) console.error(await inspect().catch(String));
    throw error;
} finally {
    writeFileSync(join(artifacts, "backend.log"), backendLog);
    writeFileSync(join(artifacts, "chrome.log"), browserLog);
    writeFileSync(join(dir, "stop"), "");
    socket?.close();
    if (backend) await until(() => backend.exitCode !== null, "backend cleanup", 15000).catch(() => {});
    for (const child of children) {
        if (child.exitCode === null) child.kill("SIGTERM");
    }
    await sleep(500);
    for (const child of children) {
        if (child.exitCode === null) child.kill("SIGKILL");
    }
    // dir is the exact mkdtemp result above, never a caller-supplied directory.
    rmSync(dir, { recursive: true, force: true });
}
