/** Built UI + isolated SQLite + synthetic provider + worker/FFmpeg. Never runs paid models.
 * Run: bun run build && env -u ADVERTISING_REAL_PROVIDER_KEY bun scripts/advertising-generation-chrome-e2e.mjs
 * Go fixture routes/provider exist only in *_test.go. Only owned children/temp dirs are cleaned.
 */
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, existsSync, rmSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { fileURLToPath } from "node:url";
const root = resolve(fileURLToPath(new URL("../..", import.meta.url)));
assert(!process.env.ADVERTISING_REAL_PROVIDER_KEY, "Run with env -u ADVERTISING_REAL_PROVIDER_KEY; this acceptance is synthetic only");
const dir = mkdtempSync(join(tmpdir(), "yingce-ad-browser-"));
const artifacts = join(root, ".superpowers/sdd/2026-11-06-advertising-workbench/browser");
mkdirSync(artifacts, { recursive: true });
const children = [];
let socket;
let backend;
let backendLog = "";
let browserLog = "";
let inspect;
let captureFailure;
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
    const requests = [];
    let lostAck;
    let loseNextApproval = false;
    socket.addEventListener("message", (event) => {
        const msg = JSON.parse(event.data);
        if (msg.id) {
            const p = pending.get(msg.id);
            if (p) {
                pending.delete(msg.id);
                clearTimeout(p.timer);
                msg.error ? p.reject(Error(JSON.stringify(msg.error))) : p.resolve(msg.result);
            }
        } else if (msg.method === "Fetch.requestPaused") {
            const p = msg.params;
            if (loseNextApproval && p.request.method === "POST" && p.request.url.endsWith("/execute") && p.responseStatusCode === 200) {
                loseNextApproval = false;
                lostAck = { url: p.request.url, body: JSON.parse(p.request.postData), status: p.responseStatusCode };
                void send("Fetch.failRequest", { requestId: p.requestId, errorReason: "ConnectionClosed" }).catch((e) => problems.push(String(e)));
            } else void send("Fetch.continueRequest", { requestId: p.requestId }).catch((e) => problems.push(String(e)));
        } else if (msg.method === "Network.requestWillBeSent" && msg.params.request.url.startsWith(origin + "/api/")) {
            const r = msg.params.request;
            requests.push({ method: r.method, path: new URL(r.url).pathname + new URL(r.url).search });
        } else if (msg.method === "Network.responseReceived" && msg.params.response.url.includes("/api/")) {
            const { status, url } = msg.params.response;
            console.log(status, new URL(url).pathname.split("/api/")[1]);
            if (url.includes("/api/dramas/ad_")) problems.push("Advertising canvas requested drama: " + status);
        } else if (msg.method === "Runtime.exceptionThrown") problems.push(msg.params.exceptionDetails.exception?.description || msg.params.exceptionDetails.text);
    });
    const send = (method, params = {}) =>
        new Promise((resolve, reject) => {
            const id = ++sequence;
            const timer = setTimeout(() => {
                pending.delete(id);
                reject(Error("CDP timeout " + method + " (socket=" + socket.readyState + ", browserExit=" + browser.exitCode + ")"));
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
    const visible = "e.checkVisibility({checkOpacity:true,checkVisibilityCSS:true}) && e.getClientRects().length > 0";
    const query = (selector) => "document.querySelector(" + JSON.stringify(selector) + ")";
    const named = (text, scope = "document", tag = "button") =>
        "Array.from((" + scope + ")?.querySelectorAll(" + JSON.stringify(tag) + ") || []).find(e => (e.getAttribute('aria-label') === " + JSON.stringify(text) + " || e.textContent.trim() === " + JSON.stringify(text) + ") && " + visible + ")";
    const point = async (expression) =>
        evaluate(
            "(()=>{const e=" +
                expression +
                ";if(!e)throw Error('not interactable: absent');if(!( " +
                visible +
                "))throw Error('not interactable: hidden');if(e.matches(':disabled')||e.closest('[aria-disabled=true]'))throw Error('not interactable: disabled');e.scrollIntoView({block:'center',inline:'center',behavior:'instant'});const r=e.getBoundingClientRect(),x=r.left+r.width/2,y=r.top+r.height/2;const hit=document.elementFromPoint(x,y);if(!hit||!e.contains(hit))throw Error('not interactable: obstructed '+(hit?.outerHTML||''));return {x,y};})()",
        );
    const tap = async (expression) => {
        console.log("CLICK", expression);
        const p = await point(expression);
        await send("Input.dispatchMouseEvent", { type: "mouseMoved", ...p });
        await send("Input.dispatchMouseEvent", { type: "mousePressed", button: "left", clickCount: 1, ...p });
        await send("Input.dispatchMouseEvent", { type: "mouseReleased", button: "left", clickCount: 1, ...p });
        await sleep(400); // Wait for the real modal enter/leave animation before the next hit-test.
    };
    const click = async (text, scope = "document", tag = "button") => {
        const expression = named(text, scope, tag);
        await until(() => evaluate("!!(" + expression + ") && !(" + expression + ").matches(':disabled')"), "visible enabled " + text);
        await tap(expression);
    };
    const input = async (selector, value) => {
        await until(() => evaluate("!!(" + query(selector) + ")"), selector);
        await point(query(selector));
        await evaluate(
            "(()=>{const e=" +
                query(selector) +
                ";e.focus();Object.getOwnPropertyDescriptor(e.tagName==='SELECT'?HTMLSelectElement.prototype:e.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set.call(e," +
                JSON.stringify(value) +
                ");e.dispatchEvent(new Event(e.tagName==='SELECT'?'change':'input',{bubbles:true}));})()",
        );
        await sleep(150);
    };
    const stage = async (label) => {
        await click(label, query('nav[aria-label="广告制作阶段"]'));
        assert.equal(await evaluate("document.querySelector('.ad-stages [aria-current=step]')?.getAttribute('aria-label')"), label);
    };
    const screenshot = async (name) => {
        const image = await send("Page.captureScreenshot", { format: "png" });
        writeFileSync(join(artifacts, name + ".png"), Buffer.from(image.data, "base64"));
    };
    captureFailure = () => screenshot("failure");
    const modal = "Array.from(document.querySelectorAll('[role=dialog]')).find(e=>" + visible + ")";
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
    await send("Fetch.enable", { patterns: [{ urlPattern: "*/batches/*/execute", requestStage: "Response" }] });
    await send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
    await send("Page.navigate", { url: origin + "/register" });
    await until(() => evaluate("!!document.querySelector('input[autocomplete=username]')"), "registration");
    await input('input[autocomplete="username"]', "test_ad_browser");
    await input('input[placeholder="至少 8 位"]', "Test-only-38!pass");
    await input('input[placeholder="再次输入密码"]', "Test-only-38!pass");
    await tap(query("label.ant-checkbox-wrapper"));
    await click("创建账号");
    await until(() => evaluate("!location.pathname.includes('register')&&!location.pathname.includes('login')"), "registered session");
    await api("/test-fixture", "POST", {});
    const ad = await api("/ad-projects", "POST", {
        name: "TEST five-stage browser export",
        draft: {
            schemaVersion: 1,
            brief: { productName: "TEST PRODUCT", description: "Synthetic test pattern in a clean studio, not a commercial", audience: "TEST", sellingPoints: ["TEST"], cta: "", goal: "awareness", durationMs: 15000 },
            brand: { name: "TEST", primaryColor: "#ffffff", logoResourceId: "test-product" },
            productResourceIds: [],
            additionalResourceIds: [],
            shots: Array.from({ length: 3 }, (_, i) => ({ id: crypto.randomUUID(), durationMs: 5000, visual: "TEST shot " + i, voiceover: "", onScreenText: "", referenceResourceIds: [] })),
        },
    });
    const base = "/ad-projects/" + ad.project.id;
    await send("Page.navigate", { url: origin + base });
    await until(() => evaluate("!!document.querySelector('.ad-stages')"), "five stages");
    await stage("素材与需求");
    // A real mounted production control must never be clicked through a hidden stage/details.
    const hidden = "Array.from(document.querySelectorAll('button')).find(e=>e.textContent.trim()==='确认脚本')";
    await assert.rejects(() => tap(hidden), /hidden|not interactable/);
    assert.equal((await api(base)).latestConfirmedRevisionId || "", "");
    const productImage = join(dir, "uploaded-product.png");
    const image = spawnSync("ffmpeg", ["-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x160", "-frames:v", "1", productImage]);
    assert.equal(image.status, 0, image.stderr?.toString());
    const uploadSelector = ".ad-asset-group:first-of-type input[type=file]";
    await point(query(uploadSelector));
    const documentNode = await send("DOM.getDocument");
    const uploadNode = await send("DOM.querySelector", { nodeId: documentNode.root.nodeId, selector: uploadSelector });
    await send("DOM.setFileInputFiles", { nodeId: uploadNode.nodeId, files: [productImage] });
    await until(async () => (await api(base)).draft.productResourceIds.length === 1, "uploaded image autosaved");
    await click("生成广告脚本");
    await stage("脚本确认");
    await click("采用候选脚本");
    await stage("素材与需求");
    await click("高级素材 · 附加素材", "document", "summary");
    const musicSelector = ".ad-asset-group:nth-of-type(3) input[type=file]";
    await point(query(musicSelector));
    const musicNode = await send("DOM.querySelector", { nodeId: documentNode.root.nodeId, selector: musicSelector });
    const musicFile = join(dir, "upload-music.wav");
    const music = spawnSync("ffmpeg", ["-v", "error", "-f", "lavfi", "-i", "sine=frequency=220:sample_rate=48000", "-t", "3", musicFile]);
    assert.equal(music.status, 0, music.stderr?.toString());
    await send("DOM.setFileInputFiles", { nodeId: musicNode.nodeId, files: [musicFile] });
    const musicDraft = await until(async () => {
        const d = await api(base);
        return d.draft.additionalResourceIds.length ? d : false;
    }, "music uploaded after planning");
    const musicId = musicDraft.draft.additionalResourceIds[0];
    await stage("脚本确认");
    await input(".ad-shot:first-of-type .grid label:nth-child(3) textarea", "TEST voice");
    await input(".ad-shot:first-of-type .grid label:nth-child(4) textarea", "TEST");
    await click("保存草稿");
    await until(async () => (await api(base)).draft.shots[0].voiceover === "TEST voice", "candidate edits persisted");
    assert.equal((await api("/test-evidence")).tasks.length, 0, "planning never creates paid task");
    await click("确认脚本，下一步");
    await screenshot("script-confirmation");
    await click("确认并冻结", modal);
    await stage("镜头制作");
    await until(() => evaluate("!!document.querySelector('[aria-label=镜头1视频模型]')"), "production opened");
    const detail = await api(base);
    const production = await api(base + "/productions", "POST", { confirmedRevisionId: detail.latestConfirmedRevisionId });
    const pbase = base + "/productions/" + production.production.id;
    const shots = production.confirmedSnapshot.snapshot.draft.shots;
    const label = (id) => "C-" + (id.length > 16 ? id.slice(0, 8) + id.slice(-8) : id);
    const card = (id, i = 0) => query('[aria-label="镜头' + (i + 1) + '制作"] [aria-label="' + label(id) + '候选"]');
    const approveCandidate = async (id, i = 0) => {
        await click("验收此候选", card(id, i));
        await input('[aria-label="验收理由"]', "TEST reviewed product, motion and timing");
        await click("批准并选用", modal);
        await until(async () => (await api(pbase)).settings.selections[shots[i].id]?.videoSubmissionId === id, "candidate selected");
    };
    for (let i = 0; i < 3; i++) {
        const scope = query('[aria-label="镜头' + (i + 1) + '制作"]');
        await input('[aria-label="镜头' + (i + 1) + '视频模型"]', "test-video");
        await input('[aria-label="镜头' + (i + 1) + '视频候选数量"]', i === 0 ? "2" : "1");
        const before = await api("/test-evidence");
        await click("获取视频报价", scope);
        await click("查看并确认批次", scope);
        assert.equal(await evaluate("(" + modal + ").querySelectorAll('input[type=checkbox]:checked').length"), 0);
        assert(await evaluate("(" + named("确认全部费用并生成", modal) + ").matches(':disabled')"));
        await assert.rejects(() => tap(named("确认全部费用并生成", modal)), /disabled/);
        assert.equal((await api("/test-evidence")).videoCalls, before.videoCalls);
        const boxes = await evaluate("(" + modal + ").querySelectorAll('input[type=checkbox]').length");
        assert.equal(boxes, i === 0 ? 2 : 1);
        for (let j = 0; j < boxes; j++) {
            await tap("(" + modal + ").querySelectorAll('input[type=checkbox]')[" + j + "]");
            if (j + 1 < boxes) assert(await evaluate("(" + named("确认全部费用并生成", modal) + ").matches(':disabled')"));
        }
        if (i === 0) {
            await screenshot("batch-two-consent");
            loseNextApproval = true;
        }
        await click("确认全部费用并生成", modal);
        const items = await until(
            async () => {
                const subs = (await api(pbase + "/submissions")).items.filter((s) => s.kind === "video" && s.shotId === shots[i].id && s.task);
                assert(!subs.some((s) => s.task.status === "failed"), "synthetic candidate failed");
                return subs.length === boxes && subs.every((s) => s.task.status === "succeeded") ? subs : false;
            },
            "video candidates " + i,
            90000,
        );
        if (i === 0) {
            assert(lostAck, "a real successful HTTP approval ACK was dropped");
            const recoveryPath = new URL(lostAck.url).pathname.replace(/\/execute$/, "");
            await until(() => requests.some((r) => r.method === "GET" && r.path === recoveryPath), "UI queried original batch after lost ACK");
            const restored = await api(recoveryPath.slice(4));
            assert.deepEqual(restored.items.map((s) => s.task.id).sort(), items.map((s) => s.task.id).sort());
            assert.equal(requests.filter((r) => r.method === "POST" && r.path === new URL(lostAck.url).pathname).length, 1);
            await until(() => evaluate("(" + scope + ").querySelectorAll('article[aria-label$=候选] video').length===2"), "two actual candidate players mounted");
            await input('[aria-label="镜头候选工作台 ' + shots[0].id + '"] .ad-production-grid label:first-child select', items[0].id);
            await input('[aria-label="镜头候选工作台 ' + shots[0].id + '"] .ad-production-grid label:nth-child(2) select', items[1].id);
            await click("播放对比视频", scope);
            await until(() => evaluate("(()=>{const v=Array.from((" + scope + ").querySelectorAll('video')).filter(e=>e.checkVisibility());return v.length===2&&v.every(e=>e.currentTime>0&&!e.paused);})()"), "comparison playback");
            await click("暂停对比视频", scope);
            await input('[aria-label="对比定位秒"]', "1");
            await click("定位对比视频", scope);
            await screenshot("candidate-comparison");
            await click("验收此候选", card(items[1].id));
            await input('[aria-label="验收理由"]', "TEST alternative not selected");
            await click("淘汰候选", modal);
            await click("记录问题", card(items[1].id));
            await input('[aria-label="问题说明"]', "TEST preserve product shape");
            await input('[aria-label="问题时间点"]', "1000");
            await click("保存问题", modal);
            await approveCandidate(items[0].id);
            await click("审计与问题");
            await click("以此问题准备返工");
            await until(() => evaluate("document.querySelector('[aria-label=镜头1候选调整]').value.includes('TEST preserve product shape')"), "issue prefilled only");
            assert.equal((await api("/test-evidence")).videoCalls, 2, "rework preparation must not call model");
        } else await approveCandidate(items[0].id, i);
        if (i === 0) {
            await evaluate("window.__candidatePlayer=(" + card(items[0].id) + ").querySelector('video');window.__candidatePlayer.loop=true;window.__candidatePlayer.play()");
        } else {
            assert(await evaluate("window.__candidatePlayer.isConnected&&!window.__candidatePlayer.paused"), "another shot completion preserves selected player");
        }
    }
    const shotScope = query('[aria-label="镜头1制作"]');
    await input('[aria-label="镜头1制作"] fieldset:nth-of-type(2) select', "test-audio");
    await input('[aria-label="镜头1制作"] fieldset:nth-of-type(2) label:nth-of-type(2) select', "alloy");
    await click("获取旁白报价", shotScope);
    await click("查看并确认批次", shotScope);
    await tap("(" + modal + ").querySelector('input[type=checkbox]')");
    await click("确认全部费用并生成", modal);
    const audio = await until(async () => (await api(pbase + "/submissions")).items.find((s) => s.kind === "audio" && s.task?.status === "succeeded"), "audio candidate", 90000);
    await click("验收此候选", card(audio.id));
    await input('[aria-label="验收理由"]', "TEST voice audition approved");
    await click("批准并选用", modal);
    await evaluate("window.__candidatePlayer.pause()");
    await stage("成片预览");
    await input('.ad-production select:has(option[value="' + musicId + '"])', musicId);
    await click("保存制作设置");
    await click("一键生成粗剪");
    const assembly = await until(async () => (await api(pbase + "/assemblies")).items[0], "assembly");
    const rendersPath = pbase + "/assemblies/" + assembly.id + "/renders";
    const render = await until(async () => (await api(rendersPath)).items.find((r) => r.available && r.task.status === "succeeded"), "real MP4", 90000);
    await click("审看此输出");
    await point(query('video[aria-label="成片MP4"]'));
    await evaluate("window.__cutPlayer=document.querySelector('video[aria-label=成片MP4]');window.__cutPlayer.muted=true;window.__cutPlayer.play()");
    await until(() => evaluate("window.__cutPlayer.currentTime>0"), "real MP4 playback");
    await input('[aria-label="成片验收理由"]', "TEST watched exact rendered output");
    await click("批准此成片");
    const cp5 = await until(async () => (await api(pbase + "/reviews")).items.find((r) => r.checkpoint === "assembly"), "CP5 receipt");
    assert.equal(cp5.resourceId, render.resourceId);
    assert.equal(cp5.renderAttemptId, render.id);
    assert.equal(cp5.assemblyId, assembly.id);
    const beforeDelivery = await api("/test-evidence");
    await stage("导出交付");
    assert(await evaluate("window.__cutPlayer===document.querySelector('video[aria-label=成片MP4]')"), "stage navigation must preserve player DOM");
    await click("确认交付此输出");
    const cp6 = await until(async () => (await api(pbase + "/reviews")).items.find((r) => r.checkpoint === "delivery"), "CP6 receipt");
    assert.equal(cp6.acceptedReviewId, cp5.id);
    for (const field of ["assemblyId", "renderAttemptId", "resourceId"]) assert.equal(cp6[field], cp5[field]);
    await evaluate("window.__cutPlayer.pause()");
    const source = await api(pbase + "/assemblies/" + assembly.id);
    assert(
        source.sources.some((s) => s.width === 160 && s.height === 284),
        "measured low-resolution input provenance",
    );
    assert(source.current && source.sourceFingerprint);
    assert(await evaluate("!Array.from(document.querySelectorAll('.ad-stages small, .ad-action-bar p')).some(e=>/待审片|待验收/.test(e.textContent))"), "completed receipts must not show hardcoded pending summaries");
    const stages = ["素材与需求", "脚本确认", "镜头制作", "成片预览", "导出交付"];
    for (const width of [1440, 390])
        for (const theme of ["light", "dark"]) {
            await send("Emulation.setDeviceMetricsOverride", { width, height: width === 390 ? 844 : 1000, deviceScaleFactor: 1, mobile: width === 390 });
            if ((await evaluate("document.documentElement.classList.contains('dark')")) !== (theme === "dark")) {
                await click("切换主题");
                await until(() => evaluate("!document.documentElement.hasAttribute('data-magicui-theme-vt')"), "theme transition finished");
            }
            for (let i = 0; i < stages.length; i++) {
                await stage(stages[i]);
                await evaluate("document.querySelector('.ad-stage-heading').scrollIntoView({block:'start',behavior:'instant'})");
                await sleep(200);
                assert(await evaluate("document.documentElement.scrollWidth<=innerWidth+1"), "horizontal overflow " + width + " " + stages[i]);
                const primary = query(".ad-action-bar .ad-actions > div button");
                if (!(await evaluate("(" + primary + ").matches(':disabled')"))) await point(primary);
                else {
                    await assert.rejects(() => point(primary), /disabled/);
                    await point(query(".ad-action-bar .ad-actions button:not([disabled])"));
                }
                await send("Input.dispatchKeyEvent", { type: "keyDown", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 });
                await send("Input.dispatchKeyEvent", { type: "keyUp", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 });
                assert(await evaluate("document.activeElement!==document.body&&document.activeElement.checkVisibility()"), "visible keyboard focus");
                await point("document.activeElement");
                await screenshot(theme + "-" + width + "-stage" + (i + 1));
                if (i === 0) {
                    await evaluate("document.querySelector('.ad-stages').scrollIntoView({block:'start',behavior:'instant'})");
                    await screenshot(theme + "-" + width + "-stage1-overview");
                }
            }
            await point(query('video[aria-label="成片MP4"]'));
            await screenshot(theme + "-" + width + "-real-cut");
            for (const text of ["批准此成片", "退回此成片", "确认交付此输出", "重新检查输出可用性"]) await point(named(text));
            await screenshot(theme + "-" + width + "-delivery-controls");
            await stage("镜头制作");
            await click("复核或淘汰已选", card(source.sourceSettings.selections[shots[0].id].videoSubmissionId));
            await point(query('[aria-label="验收理由"]'));
            assert(await evaluate("(" + modal + ").contains(document.activeElement)"), "modal owns focus");
            await screenshot(theme + "-" + width + "-review-modal");
            await click("Close", modal);
            await point("document.activeElement");
            await stage("导出交付");
        }
    assert(await evaluate("window.__cutPlayer===document.querySelector('video[aria-label=成片MP4]')"), "polling and all stages preserve player DOM");
    const afterDelivery = await api("/test-evidence");
    assert.equal(afterDelivery.tasks.length, beforeDelivery.tasks.length);
    assert.equal(afterDelivery.videoCalls, beforeDelivery.videoCalls);
    assert.equal(afterDelivery.audioCalls, beforeDelivery.audioCalls);
    assert.deepEqual(afterDelivery.account, beforeDelivery.account);
    assert.equal(afterDelivery.billingOrders, beforeDelivery.billingOrders);
    assert.equal(afterDelivery.billingOrders, 5);
    assert.equal((await api(rendersPath)).items.length, 1, "CP5/CP6/navigation reuse one render");
    const url = await evaluate("(" + named("下载此输出 MP4", query(".ad-cut-review"), "a") + ").href");
    await send("Browser.setDownloadBehavior", { behavior: "allow", downloadPath: join(dir, "downloads") });
    await click("下载此输出 MP4", query(".ad-cut-review"), "a");
    const cookies = await send("Network.getCookies", { urls: [origin] });
    const response = await fetch(url, { headers: { Cookie: cookies.cookies.map((c) => c.name + "=" + c.value).join("; ") } });
    assert(response.ok, "same output download");
    const mp4 = join(artifacts, "TEST-output.mp4");
    writeFileSync(mp4, Buffer.from(await response.arrayBuffer()));
    const probe = spawnSync("ffprobe", ["-v", "error", "-show_streams", "-show_format", "-of", "json", mp4], { encoding: "utf8" });
    assert.equal(probe.status, 0);
    const metadata = JSON.parse(probe.stdout);
    assert(metadata.streams.some((s) => s.codec_type === "video" && s.codec_name === "h264" && s.width === 1080 && s.height === 1920));
    assert(metadata.streams.some((s) => s.codec_type === "audio" && s.codec_name === "aac"));
    assert(Math.abs(Number(metadata.format.duration) - 15) < 0.034);
    const events = (await api(base + "/events?pageSize=100")).items;
    const reviews = (await api(pbase + "/reviews?pageSize=100")).items;
    const issues = (await api(pbase + "/issues")).items;
    assert(issues.some((i) => i.note === "TEST preserve product shape" && i.status === "open" && i.atMs === 1000));
    assert(reviews.every((r) => r.actorId && Date.parse(r.createdAt) && r.clientOperationId));
    assert(!JSON.stringify(events).includes("test-not-a-secret"));
    assert(!JSON.stringify(events).includes("http://"));
    assert.equal(afterDelivery.videoCalls, 4);
    assert.equal(afterDelivery.audioCalls, 1);
    assert.equal(afterDelivery.account.reservedMicrocredits, 0);
    assert.equal(afterDelivery.account.availableMicrocredits, 9970);
    writeFileSync(
        join(artifacts, "evidence.json"),
        JSON.stringify(
            {
                provider: "synthetic",
                hiddenControlRejected: true,
                lostAck: { path: new URL(lostAck.url).pathname, status: lostAck.status },
                cp5,
                cp6,
                issues,
                events,
                reviews,
                sources: source.sources,
                sourceFingerprint: source.sourceFingerprint,
                metadata,
                account: afterDelivery.account,
                tasks: afterDelivery.tasks.map((t) => ({ id: t.id, type: t.type, status: t.status })),
                requests,
                problems,
            },
            null,
            2,
        ),
    );
    await click("打开高级时间线（独立编辑分支）", "document", "a");
    await until(() => evaluate("location.pathname.startsWith('/canvas/')"), "real advanced canvas route");
    assert.deepEqual(problems, []);
    writeFileSync(join(dir, "stop"), "");
    await until(() => backend.exitCode !== null, "clean backend shutdown", 15000);
    assert.equal(backend.exitCode, 0, backendLog);
    console.log("PASS: real built five-stage React workflow; visible CDP/hidden rejection; 2-candidate consent/lost-ACK GET recovery/review/compare/issues/rework; CP5/CP6 same MP4; 1440/390 light/dark; H264/AAC 15s 1080x1920; TEST ONLY");
} catch (error) {
    if (captureFailure) await captureFailure().catch(() => {});
    if (inspect) console.error(await inspect().catch(String));
    console.error(error);
    process.exitCode = 1;
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
    // This bounded CLI owns all children; Bun may retain native handles after CDP closes.
    process.exit(process.exitCode || 0);
}
