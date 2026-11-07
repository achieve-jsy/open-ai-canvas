import { expect, test } from "bun:test";
import { Window } from "happy-dom";
import { spawnSync } from "node:child_process";

test("production requires explicit entry, fee approval and adoption", async () => {
    if (!process.env.AD_GENERATION_DOM_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path, "-t", "production requires"], { env: { ...process.env, AD_GENERATION_DOM_CHECK: "1" }, encoding: "utf8", timeout: 60000 });
        expect(child.stderr + child.stdout).not.toContain("(fail)");
        expect(child.status).toBe(0);
        return;
    }
    const dom = new Window({ url: "http://localhost" });
    for (const [key, value] of Object.entries({
        window: dom,
        document: dom.document,
        navigator: dom.navigator,
        HTMLElement: dom.HTMLElement,
        Element: dom.Element,
        SVGElement: dom.SVGElement,
        ShadowRoot: dom.ShadowRoot,
        Node: dom.Node,
        getComputedStyle: dom.getComputedStyle.bind(dom),
        localStorage: dom.localStorage,
        IS_REACT_ACT_ENVIRONMENT: true,
    }))
        Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
    const { act } = await import("react");
    const { createRoot } = await import("react-dom/client");
    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { MemoryRouter } = await import("react-router");
    const { AdvertisingProduction } = await import("../src/pages/ad-projects/advertising-production");
    const { emptyAdvertisingDraft } = await import("../src/services/api/advertising-project");
    const { apiClient } = await import("../src/services/api/request");
    const { useUserStore } = await import("../src/stores/use-user-store");
    const { setActiveUserScope } = await import("../src/lib/user-scope");
    setActiveUserScope("owner");
    useUserStore.setState({ user: { id: "owner" } as any });
    const draft = emptyAdvertisingDraft();
    draft.brief.durationMs = 15000;
    draft.shots = [{ id: "shot", durationMs: 5000, visual: "TEST", voiceover: "", onScreenText: "TEST", referenceResourceIds: [] }];
    let production: any = {
        production: { id: "p", revision: 1, confirmedRevisionId: "v" },
        settings: { fit: "pad", ttsModelId: "", voiceId: "", selections: {}, musicResourceId: "", musicVolume: 0.15 },
        confirmedSnapshot: { id: "v", sourceRevision: 1, snapshot: { name: "TEST", draft, resources: [] } },
    };
    let submissions: any[] = [];
    let batch: any;
    let reviewResult: any;
    let executed = 0;
    let lost = true;
    let prepared = 0;
    let failSave = false;
    let rejectAdoption = false;
    let loseAdoption = false;
    let failRead = false;
    const settingsWrites: any[] = [];
    let deferSave = false;
    let finishSave: (() => void) | undefined;
    let expired = false;
    let finishQuote: (() => void) | undefined;
    let deferQuote = false;
    let failAssembly = false;
    const assemblyKeys: string[] = [];
    const renderKeys: string[] = [];
    const calls: string[] = [];
    let failRender = true;
    const original = apiClient.defaults.adapter;
    apiClient.defaults.adapter = async (config) => {
        const url = config.url || "";
        const body = config.data ? JSON.parse(config.data) : {};
        let data: any = {};
        let reason = "";
        if (url === "/model-catalog")
            data = {
                channels: [
                    {
                        id: "ch",
                        models: [{ id: "m", displayName: "Test video", capability: "video", available: true, capabilityConfig: { video: { ratios: ["9:16"], duration: { selection: "enum", values: [5] }, references: { minImages: 0, maxImages: 2 } } } }],
                    },
                ],
            };
        else if (url === "/plugins/catalog") data = { providers: [] };
        else if (url.endsWith("/activity")) data = { items: [], batches: [] };
        else if (url.endsWith("/reviews") && config.method === "post") {
            if (deferSave)
                await new Promise<void>((resolve) => {
                    finishSave = resolve;
                });
            if (rejectAdoption) reason = "advertising_invalid_document";
            else {
                expect(body.expectedProductionRevision).toBe(production.production.revision);
                production = {
                    ...production,
                    production: { ...production.production, revision: production.production.revision + 1 },
                    settings: { ...production.settings, selections: { shot: { videoSubmissionId: body.submissionId, audioSubmissionId: "", videoStartMs: 0 } } },
                };
                reviewResult = { review: { ...body, id: "review" }, production: structuredClone(production) };
                data = reviewResult;
                if (loseAdoption) {
                    loseAdoption = false;
                    throw new Error("adoption response lost");
                }
            }
        } else if (url.endsWith("/reviews")) {
            if (failRead) throw new Error("offline read");
            data = config.params?.clientOperationId ? reviewResult : { items: reviewResult ? [reviewResult.review] : [], total: reviewResult ? 1 : 0 };
            if (!data) reason = "advertising_not_found";
        } else if (url.endsWith("/execute")) {
            if (expired) {
                reason = "advertising_quote_expired";
            } else {
                executed++;
                submissions[0].task = { id: "task", status: "succeeded", resultJson: JSON.stringify({ video: { storageKey: "resource:video", durationMs: 5000 } }) };
                batch.batch.approvedAt = new Date().toISOString();
                data = batch;
                if (lost) {
                    lost = false;
                    throw new Error("response lost");
                }
            }
        } else if (url.endsWith("/batches") && config.method === "post") {
            if (deferQuote)
                await new Promise<void>((resolve) => {
                    finishQuote = resolve;
                });
            prepared++;
            data = { id: "s", productionId: "p", shotId: "shot", kind: "video", publicRequest: { modelId: "m", prompt: "TEST" }, quote: { quoteHash: "hash", expiresAt: new Date(Date.now() + 60000).toISOString(), amountMicrocredits: 1000 } };
            submissions = [data];
            batch = { batch: { id: "batch" + prepared, productionId: "p", shotId: "shot", kind: "video", count: 1, clientOperationId: body.clientOperationId, adjustment: "" }, items: submissions };
            data = batch;
        } else if (url.endsWith("/batches")) data = config.params?.clientOperationId ? batch : { items: batch ? [batch] : [], total: batch ? 1 : 0 };
        else if (url.includes("/batches/")) data = batch;
        else if (url.endsWith("/submissions")) data = { items: submissions, page: 1, pageSize: 20, total: submissions.length };
        else if (url.endsWith("/assemblies") && config.method === "post") {
            calls.push("assembly");
            assemblyKeys.push(body.clientOperationId);
            if (failAssembly) throw new Error("镜头1视频实际4999ms，目标5000ms");
            data = { id: "a", current: true, sourceProductionRevision: production.production.revision, selectionHash: "selected", timeline: { version: 2, tracks: [], clips: [], durationMs: 5000 } };
        } else if (url.endsWith("/assemblies")) data = { items: [], page: 1, pageSize: 20, total: 0 };
        else if (url.endsWith("/assemblies/a")) data = { id: "a", current: true, sourceProductionRevision: production.production.revision, selectionHash: "selected", timeline: { version: 2, tracks: [], clips: [], durationMs: 5000 } };
        else if (url.endsWith("/renders") && config.method === "post") {
            calls.push("render");
            renderKeys.push(body.clientOperationId);
            if (failRender) throw new Error("render response lost");
            data = { id: "r", assemblyId: "a", task: { id: "task", status: "pending" } };
        } else if (url.endsWith("/renders")) data = { items: [], page: 1, pageSize: 20, total: 0 };
        else if (url.includes("/resources/")) data = { resource: { id: "video", status: "ready", mimeType: "video/mp4", durationMs: 5000 } };
        else if (config.method === "patch") {
            settingsWrites.push(body.settings);
            expect(body.expectedRevision).toBe(production.production.revision);
            if (deferSave)
                await new Promise<void>((resolve) => {
                    finishSave = resolve;
                });
            if (rejectAdoption) reason = "advertising_invalid_document";
            else if (failSave) reason = "advertising_production_conflict";
            else {
                production = { ...production, production: { ...production.production, revision: production.production.revision + 1 }, settings: body.settings };
                data = production;
                if (loseAdoption) {
                    loseAdoption = false;
                    throw new Error("adoption response lost");
                }
            }
        } else {
            if (failRead && config.method === "get") throw new Error("offline read");
            data = structuredClone(production);
        }
        return { config, headers: {}, status: 200, statusText: "OK", data: reason ? { code: 409, reason, msg: reason } : { code: 0, data } };
    };
    const host = dom.document.createElement("div");
    dom.document.body.append(host);
    const root = createRoot(host as unknown as HTMLElement);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const render = (confirmedRevisionId?: string) =>
        root.render(
            <QueryClientProvider client={client}>
                <MemoryRouter>
                    <AdvertisingProduction key={confirmedRevisionId || "empty"} projectId="ad" userId="owner" confirmedRevisionId={confirmedRevisionId} />
                </MemoryRouter>
            </QueryClientProvider>,
        );
    const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 40)));
    const button = (text: string) => Array.from(dom.document.querySelectorAll("button")).find((b) => b.textContent?.replaceAll(" ", "") === text)!;
    try {
        await act(() => render());
        await settle();
        expect(host.textContent).toContain("确认脚本后");
        expect(prepared).toBe(0);
        await act(() => render("v"));
        await settle();
        expect(button("进入此版本制作")).toBeTruthy();
        const { localForageStorageForScope } = await import("../src/lib/localforage-storage");
        const storage = localForageStorageForScope("owner");
        await storage.setItem("advertising-autosave:owner:ad:production:v", JSON.stringify({ base: { revision: 1, value: production.settings }, value: { ...production.settings, selections: { shot: { videoSubmissionId: "bad" } } } }));
        await act(() => button("进入此版本制作").click());
        await settle();
        await settle();
        expect(host.textContent).not.toContain("已与服务端同步");
        expect(host.querySelector<HTMLInputElement>('input[step="0.05"]')?.value).toBe("0.15");
        expect(button("组装新时间线").disabled).toBe(true);
        expect(button("一键生成粗剪").disabled).toBe(true);
        const select = host.querySelector('[aria-label="镜头1视频模型"]')!;
        await act(() => {
            (select as any).value = "m";
            select.dispatchEvent(new dom.Event("change", { bubbles: true }));
        });
        await act(() => button("获取视频报价").click());
        await settle();
        expect(prepared).toBe(1);
        expect(executed).toBe(0);
        await act(() => button("查看并确认批次").click());
        await settle();
        await act(() => {
            dom.document.querySelector<HTMLInputElement>('[role="dialog"] input[type="checkbox"]')!.click();
        });
        await act(() => button("确认全部费用并生成").click());
        await settle();
        expect(executed).toBe(1);
        expect(prepared).toBe(1);
        expect(executed).toBe(1); // Lost ACK is recovered by GET, not another execute.
        await settle();
        expect(button("组装新时间线").disabled).toBe(true);
        const volumeEdit = async (value: string) => {
            await act(() => {
                const volume = host.querySelector<HTMLInputElement>('input[step="0.05"]')!;
                Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, "value")!.set!.call(volume, value);
                volume.dispatchEvent(new dom.Event("input", { bubbles: true }));
            });
        };
        const approve = async () => {
            if (!dom.document.querySelector('[aria-label="验收理由"]')) {
                await act(() => button("验收此候选").click());
                await settle();
                await act(() => {
                    const field = dom.document.querySelector('[aria-label="验收理由"]')!;
                    Object.getOwnPropertyDescriptor(dom.HTMLTextAreaElement.prototype, "value")!.set!.call(field, "checked");
                    field.dispatchEvent(new dom.Event("input", { bubbles: true }));
                });
            }
            await act(() => button("批准并选用").click());
        };
        rejectAdoption = true;
        deferSave = true;
        await approve();
        await settle();
        await volumeEdit("0.3");
        deferSave = false;
        await act(() => finishSave!());
        await settle();
        expect(host.textContent).not.toContain("· 已采用");
        expect(host.querySelector<HTMLInputElement>('input[step="0.05"]')!.value).toBe("0.3");
        rejectAdoption = false;
        await volumeEdit("0.35");
        await act(() => new Promise((resolve) => setTimeout(resolve, 760)));
        await settle();
        expect(production.settings.selections.shot).toBeUndefined();
        expect(production.settings.musicVolume).toBe(0.35);
        expect(settingsWrites.at(-1).selections.shot).toBeUndefined();
        deferSave = true;
        loseAdoption = true;
        await approve();
        await settle();
        expect(host.textContent).not.toContain("· 已采用");
        await act(() => {
            const volume = host.querySelector<HTMLInputElement>('input[step="0.05"]')!;
            Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, "value")!.set!.call(volume, "0.2");
            volume.dispatchEvent(new dom.Event("input", { bubbles: true }));
        });
        deferSave = false;
        failRead = true;
        await act(() => finishSave!());
        await settle();
        expect(host.textContent).not.toContain("· 已采用");
        const blockedWrites = settingsWrites.length;
        await volumeEdit("0.25");
        await act(() => new Promise((resolve) => setTimeout(resolve, 760)));
        expect(settingsWrites).toHaveLength(blockedWrites);
        failRead = false;
        await act(() => button("重新读取服务端制作").click());
        await settle();
        await act(() => button("保留本地设置继续编辑").click());
        await settle();
        await act(() => button("保存制作设置").click());
        await settle();
        await act(() => button("查询原验收").click());
        await settle();
        expect(production.settings.musicVolume).toBe(0.25);
        expect(production.settings.selections.shot.videoSubmissionId).toBe("s");
        expect(button("复核或淘汰已选")).toBeTruthy(); // Current selection cannot accidentally be re-approved at trim zero.
        expect(button("组装新时间线").disabled).toBe(false);
        expect(button("一键生成粗剪").disabled).toBe(false);
        failAssembly = true;
        await act(() => button("组装新时间线").click());
        await settle();
        expect(host.textContent).toContain("镜头1视频实际4999ms，目标5000ms");
        await act(() => button("组装新时间线").click());
        await settle();
        expect(assemblyKeys.length).toBe(2);
        expect(assemblyKeys[0]).toBe(assemblyKeys[1]);
        await act(() => root.render(<div />));
        await act(() => render("v"));
        await settle();
        await act(() => button("进入此版本制作").click());
        await settle();
        await settle();
        expect(host.textContent).toContain("已采用");
        await act(() => button("组装新时间线").click());
        await settle();
        expect(assemblyKeys[2]).toBe(assemblyKeys[0]);
        failAssembly = false;
        const fit = () => Array.from(host.querySelectorAll("select")).find((select) => select.parentElement?.textContent?.includes("画幅适配"))!;
        await act(() => {
            fit().value = "crop";
            fit().dispatchEvent(new dom.Event("change", { bubbles: true }));
        });
        await act(() => button("一键生成粗剪").click());
        await settle();
        expect(calls.slice(-2)).toEqual(["assembly", "render"]);
        expect(host.textContent).toContain("render response lost");
        const count = assemblyKeys.length;
        await act(() => root.render(<div />));
        await act(() => render("v"));
        await settle();
        await act(() => button("进入此版本制作").click());
        await settle();
        await settle();
        expect(fit().value).toBe("crop");
        failRender = false;
        await act(() => button("一键生成粗剪").click());
        await settle();
        expect(assemblyKeys).toHaveLength(count);
        expect(renderKeys).toHaveLength(2);
        expect(renderKeys[0]).toBe(renderKeys[1]);
        expect(executed).toBe(1);
        await act(() => button("组装新时间线").click());
        await settle();
        expect(assemblyKeys).toHaveLength(count + 1);
        expect(renderKeys).toHaveLength(2);
        failSave = true;
        await act(() => {
            const volume = host.querySelector<HTMLInputElement>('input[step="0.05"]')!;
            Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, "value")!.set!.call(volume, "0.45");
            volume.dispatchEvent(new dom.Event("input", { bubbles: true }));
        });
        await act(() => button("保存制作设置").click());
        await settle();
        expect(host.textContent).toContain("本地设置保留");
        expect(production.settings.selections.shot.videoSubmissionId).toBe("s");
        await act(() => button("采用服务端设置").click());
        await settle();
        failSave = false;
        const chooseModel = async () => {
            const el = host.querySelector('[aria-label="镜头1视频模型"]')!;
            await act(() => {
                (el as any).value = "m";
                el.dispatchEvent(new dom.Event("change", { bubbles: true }));
            });
        };
        await chooseModel();
        await act(() => button("获取视频报价").click());
        await settle();
        expired = true;
        await act(() => button("查看并确认批次").click());
        await settle();
        await act(() => {
            dom.document.querySelector<HTMLInputElement>('[role="dialog"] input[type="checkbox"]')!.click();
        });
        await act(() => button("确认全部费用并生成").click());
        await settle();
        expect(dom.document.body.textContent).toContain("报价已过期");
        expect(executed).toBe(1);
        await act(() => button("暂不生成").click());
        await settle();
        expired = false;
        await act(() => button("获取视频报价").click());
        await settle();
        expect(prepared).toBe(3);
        deferQuote = true;
        await act(() => button("获取视频报价").click());
        await settle();
        await act(() => render("v2"));
        await settle();
        expect(button("进入此版本制作")).toBeTruthy();
        await act(() => {
            finishQuote?.();
        });
        await settle();
        expect(host.textContent).not.toContain("预授权上限");
        expect(executed).toBe(1);
    } finally {
        await act(() => root.unmount());
        client.clear();
        apiClient.defaults.adapter = original;
        await dom.happyDOM.close();
    }
}, 65000);

test("mounted shot inherits saved TTS defaults without resetting overrides or frozen quotes", async () => {
    if (!process.env.AD_SHOT_DEFAULTS_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path, "-t", "mounted shot inherits"], { env: { ...process.env, AD_SHOT_DEFAULTS_CHECK: "1" }, encoding: "utf8", timeout: 15000 });
        expect(child.stderr + child.stdout).not.toContain("(fail)");
        expect(child.status).toBe(0);
        return;
    }
    const dom = new Window({ url: "http://localhost" });
    for (const [key, value] of Object.entries({
        window: dom,
        document: dom.document,
        navigator: dom.navigator,
        HTMLElement: dom.HTMLElement,
        Element: dom.Element,
        SVGElement: dom.SVGElement,
        ShadowRoot: dom.ShadowRoot,
        Node: dom.Node,
        getComputedStyle: dom.getComputedStyle.bind(dom),
        localStorage: dom.localStorage,
        IS_REACT_ACT_ENVIRONMENT: true,
    }))
        Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
    const { act } = await import("react");
    const { createRoot } = await import("react-dom/client");
    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { AdvertisingShotGeneration } = await import("../src/pages/ad-projects/advertising-shot-generation");
    const { apiClient } = await import("../src/services/api/request");
    const original = apiClient.defaults.adapter;

    const host = dom.document.createElement("div");
    dom.document.body.append(host);
    const root = createRoot(host as unknown as HTMLElement);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const requests: any[] = [];
    const executed: any[] = [];
    const frozen: any = {
        id: "quoted",
        productionId: "p",
        shotId: "shot",
        kind: "audio",
        publicRequest: { modelId: "old", voiceId: "alloy", format: "mp3" },
        quote: { quoteHash: "old-hash", amountMicrocredits: 1000, expiresAt: new Date(Date.now() + 60000).toISOString() },
    };
    const props: any = {
        shot: { id: "shot", durationMs: 5000, visual: "product", voiceover: "voice", onScreenText: "", referenceResourceIds: [] },
        index: 0,
        models: [
            { id: "video", displayName: "Video", capability: "video", available: true, capabilityConfig: { video: { ratios: ["9:16"], duration: { selection: "enum", values: [5] } } } },
            ...["old", "new"].map((id) => ({ id, displayName: id, capability: "audio", available: true, protocol: "tts" })),
        ],
        protocols: [
            {
                value: "tts",
                parameters: [
                    { name: "voice", values: ["alloy", "nova"] },
                    { name: "format", values: ["mp3", "wav"] },
                ],
            },
        ],
        submissions: [
            frozen,
            { id: "candidate", productionId: "p", shotId: "shot", kind: "video", publicRequest: { modelId: "video" }, task: { id: "task", status: "succeeded", resultJson: JSON.stringify({ video: { storageKey: "resource:video", durationMs: 5000 } }) } },
        ],
        projectId: "ad",
        production: { production: { id: "p" }, settings: { ttsModelId: "", voiceId: "", selections: {} }, confirmedSnapshot: { snapshot: { draft: { productResourceIds: [], shots: [{ id: "shot", durationMs: 5000 }] } } } },
        disabled: false,
        current: () => true,
        flush: async () => {},
        review: async () => {},
    };
    let batch: any;
    apiClient.defaults.adapter = async (config) => {
        const url = config.url || "";
        const body = config.data ? JSON.parse(config.data) : {};
        let data: any;
        if (url.endsWith("/activity")) data = { items: [], batches: [] };
        else if (url.endsWith("/batches") && config.method === "post") {
            const { clientOperationId, ...request } = body;
            requests.push(request);
            batch = {
                batch: { id: "b" + requests.length, clientOperationId, shotId: "shot", productionId: "p", kind: body.kind, count: 1, adjustment: body.adjustment },
                items: [{ ...frozen, publicRequest: { modelId: body.modelId, voiceId: body.voiceId, format: body.format }, kind: body.kind }],
            };
            data = batch;
        } else if (url.endsWith("/execute")) {
            executed.push(body.items);
            batch.batch.approvedAt = new Date().toISOString();
            data = batch;
        } else if (url.endsWith("/batches")) data = { items: batch ? [batch] : [], total: batch ? 1 : 0 };
        else if (url.endsWith("/reviews")) data = { items: [], total: 0 };
        else data = { resource: { id: "video", status: "ready", mimeType: "video/mp4", durationMs: 5000 } };
        return { config, headers: {}, status: 200, statusText: "OK", data: { code: 0, data } };
    };
    const render = () =>
        root.render(
            <QueryClientProvider client={client}>
                <AdvertisingShotGeneration {...props} />
            </QueryClientProvider>,
        );
    const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 40)));
    const button = (text: string) => Array.from(dom.document.querySelectorAll("button")).find((b) => b.textContent?.replaceAll(" ", "") === text)!;
    const choose = async (index: number, value: string) =>
        act(() => {
            const select = host.querySelectorAll("select")[index];
            select.value = value;
            select.dispatchEvent(new dom.Event("change", { bubbles: true }));
        });
    try {
        await act(render);
        await settle();
        await settle();
        const selects = host.querySelectorAll("select");
        const player = host.querySelector("video");
        expect(player).toBeTruthy();
        await choose(0, "video");
        props.production = { ...props.production, settings: { ...props.production.settings, ttsModelId: "old", voiceId: "alloy" } };
        await act(render);
        expect(host.querySelectorAll("select")[2]).toBe(selects[2]);
        expect(selects[2].value).toBe("old");
        expect(selects[3].value).toBe("alloy");
        await act(() => button("获取旁白报价").click());
        await settle();
        expect(requests.at(-1)).toEqual({ count: 1, adjustment: "", shotId: "shot", kind: "audio", modelId: "old", voiceId: "alloy", format: "mp3" });
        await act(() => button("查看并确认批次").click());
        await settle();
        const modal = dom.document.querySelector('[role="dialog"]');
        props.production = { ...props.production, settings: { ...props.production.settings, ttsModelId: "new", voiceId: "nova" } };
        await act(render);
        expect(selects[2].value).toBe("new");
        expect(selects[3].value).toBe("nova");
        expect(selects[0].value).toBe("video");
        expect(host.querySelector("video")).toBe(player);
        expect(dom.document.querySelector('[role="dialog"]')).toBe(modal);
        expect(modal?.textContent).toContain('"modelId": "old"');
        expect(executed).toEqual([]);
        await act(() => {
            dom.document.querySelector<HTMLInputElement>('[role="dialog"] input[type="checkbox"]')!.click();
        });
        await act(() => button("确认全部费用并生成").click());
        await settle();
        expect(executed).toEqual([[{ submissionId: "quoted", quoteHash: "old-hash" }]]);
        await act(() => button("获取旁白报价").click());
        await settle();
        expect(requests.at(-1)).toEqual({ count: 1, adjustment: "", shotId: "shot", kind: "audio", modelId: "new", voiceId: "nova", format: "mp3" });
        // A voice-only override must not freeze the inherited model.
        await choose(3, "alloy");
        props.production = { ...props.production, settings: { ...props.production.settings, ttsModelId: "old", voiceId: "nova" } };
        await act(render);
        expect(selects[2].value).toBe("old");
        expect(selects[3].value).toBe("alloy");
        await choose(2, "new"); // Explicit model changes require an explicit new voice.
        expect(selects[3].value).toBe("");
        props.production = { ...props.production, settings: { ...props.production.settings, ttsModelId: "old", voiceId: "alloy" } };
        await act(render);
        expect(selects[2].value).toBe("new");
        expect(selects[3].value).toBe("");
        expect(button("获取旁白报价").disabled).toBe(true);
        await choose(3, "nova");
        await choose(4, "wav");
        await act(render);
        await act(() => button("获取旁白报价").click());
        await settle();
        expect(requests.at(-1)).toEqual({ count: 1, adjustment: "", shotId: "shot", kind: "audio", modelId: "new", voiceId: "nova", format: "wav" });
        props.shot = { ...props.shot, voiceover: "" };
        props.production = { ...props.production, settings: { ...props.production.settings, ttsModelId: "", voiceId: "" } };
        await act(render);
        expect(button("获取旁白报价")).toBeUndefined();
        expect(button("获取视频报价").disabled).toBe(false);
        await act(() => button("获取视频报价").click());
        await settle();
        expect(requests.at(-1)).toEqual({ count: 1, adjustment: "", shotId: "shot", kind: "video", modelId: "video" });
        expect(executed).toHaveLength(1);
    } finally {
        await act(() => root.unmount());
        client.clear();
        apiClient.defaults.adapter = original;
        await dom.happyDOM.close();
    }
}, 20000);
