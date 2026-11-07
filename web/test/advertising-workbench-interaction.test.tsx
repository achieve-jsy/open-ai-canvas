import { expect, test } from "bun:test";
import { Window } from "happy-dom";
import { spawnSync } from "node:child_process";

// Removing explicit full-batch consent or replaying review intent through settings breaks this consumer.
test("workbench batches require full consent and reviews never replay through autosave", async () => {
    if (!process.env.AD_WORKBENCH_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path], { env: { ...process.env, AD_WORKBENCH_CHECK: "1" }, encoding: "utf8", timeout: 60000 });
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
    draft.shots = [{ id: "shot", durationMs: 5000, visual: "product", voiceover: "", onScreenText: "", referenceResourceIds: [] }];
    let production: any = {
        production: { id: "p", revision: 1, confirmedRevisionId: "v" },
        settings: { ttsModelId: "", voiceId: "", selections: {}, musicResourceId: "", musicVolume: 0.15 },
        confirmedSnapshot: { id: "v", sourceRevision: 1, snapshot: { name: "Test", draft, resources: [] } },
    };
    let batches: any[] = [];
    let activity: any[] = [];
    const calls: { method: string; url: string; body: any; params: any }[] = [];
    let loseExecute = true;
    let rejectReview = true;
    let holdReview = false;
    let loseReviewBeforeAdmission = true;
    let releaseReview: (() => void) | undefined;
    const reviews: any[] = [];
    const issues: any[] = [];
    let oldReview: any;
    let loseIssueBeforeAdmission = true;
    let losePrepareBeforeAdmission = false;
    const original = apiClient.defaults.adapter;
    apiClient.defaults.adapter = async (config) => {
        const url = config.url || "";
        const body = config.data ? JSON.parse(config.data) : {};
        calls.push({ method: config.method || "", url, body, params: config.params });
        let data: any = {};
        let reason = "";
        if (url === "/model-catalog") data = { channels: [{ models: [{ id: "m", displayName: "Video", capability: "video", available: true, capabilityConfig: { video: { ratios: ["9:16"], duration: { selection: "enum", values: [5] } } } }] }] };
        else if (url === "/plugins/catalog") data = { providers: [] };
        else if (url.endsWith("/activity")) data = { items: activity, batches: [] };
        else if (url.endsWith("/batches") && config.method === "post") {
            if (losePrepareBeforeAdmission) {
                losePrepareBeforeAdmission = false;
                throw new (await import("../src/services/api/request")).ApiError("server uncertain", { status: 503, retryable: true });
            }
            const b = {
                batch: { id: "b" + (batches.length + 1), productionId: "p", clientOperationId: body.clientOperationId, shotId: "shot", kind: "video", count: body.count, adjustment: body.adjustment, createdAt: new Date().toISOString() },
                items: [] as any[],
            };
            b.items = Array.from({ length: body.count }, (_, i) => ({
                id: b.batch.id + "s" + i,
                batchId: b.batch.id,
                candidateIndex: i + 1,
                productionId: "p",
                shotId: "shot",
                kind: "video",
                publicRequest: { modelId: "m", prompt: "product " + body.adjustment },
                quote: { quoteHash: "hash" + i, amountMicrocredits: 100, expiresAt: new Date(Date.now() + 60000).toISOString() },
            }));
            batches.unshift(b);
            data = b;
        } else if (url.endsWith("/execute")) {
            const b = batches.find((b) => url.includes("/" + b.batch.id + "/"));
            b.batch.approvedAt = new Date().toISOString();
            b.items.forEach((s: any, i: number) => {
                s.task = { id: "task" + s.id, type: "video_test", status: i === 0 ? "succeeded" : "failed", resultJson: JSON.stringify({ video: { storageKey: "resource:" + s.id, durationMs: 5000 } }) };
            });
            data = b;
            if (loseExecute) {
                loseExecute = false;
                throw new Error("execute ACK lost");
            }
        } else if (url.endsWith("/batches")) {
            data = config.params?.clientOperationId ? batches.find((b) => b.batch.clientOperationId === config.params.clientOperationId) : { items: batches, total: batches.length, page: 1, pageSize: 20 };
            if (!data) reason = "not_found";
        } else if (url.includes("/batches/")) data = batches.find((b) => url.endsWith("/" + b.batch.id));
        else if (url.endsWith("/submissions")) data = { items: batches.flatMap((b) => b.items), total: batches.reduce((n, b) => n + b.items.length, 0), page: 1, pageSize: 20 };
        else if (url.endsWith("/reviews") && config.method === "post") {
            if (holdReview)
                await new Promise<void>((resolve) => {
                    releaseReview = resolve;
                });
            if (rejectReview) reason = "advertising_invalid_document";
            else {
                if (loseReviewBeforeAdmission) {
                    loseReviewBeforeAdmission = false;
                    throw new (await import("../src/services/api/request")).ApiError("review server uncertain", { status: 503, retryable: true });
                }
                expect(body.expectedProductionRevision).toBe(production.production.revision);
                production = {
                    ...production,
                    production: { ...production.production, revision: production.production.revision + 1 },
                    settings: { ...production.settings, selections: { shot: { videoSubmissionId: body.submissionId, audioSubmissionId: "", videoStartMs: 0 } } },
                };
                const review = { ...body, id: "review", productionId: "p", createdAt: new Date().toISOString() };
                reviews.push(review);
                data = { review, production: structuredClone(production) };
                oldReview = structuredClone(data);
            }
        } else if (url.endsWith("/reviews")) {
            data = config.params?.clientOperationId ? oldReview : { items: reviews, total: reviews.length };
            if (!data) reason = "not_found";
        } else if (url.endsWith("/issues") && config.method === "post") {
            if (loseIssueBeforeAdmission) {
                loseIssueBeforeAdmission = false;
                throw new (await import("../src/services/api/request")).ApiError("issue ACK unknown", { status: 503, retryable: true });
            }
            data = { ...body, id: "issue", productionId: "p", revision: 1, status: "open", createdAt: new Date().toISOString() };
            issues.push(data);
        } else if (url.endsWith("/issues")) {
            data = config.params?.clientOperationId ? issues.find((i) => i.clientOperationId === config.params.clientOperationId) : { items: issues, total: issues.length };
            if (!data) reason = "not_found";
        } else if (url.endsWith("/events")) data = { items: [], total: 0 };
        else if (url.includes("/resources/")) data = { resource: { id: url.split("/").at(-1), status: "ready", mimeType: "video/mp4", durationMs: 6000 } };
        else if (url.endsWith("/assemblies") || url.endsWith("/renders")) data = { items: [], total: 0 };
        else if (config.method === "patch") {
            expect(body.expectedRevision).toBe(production.production.revision);
            production = { ...production, production: { ...production.production, revision: production.production.revision + 1 }, settings: body.settings };
            data = structuredClone(production);
        } else data = structuredClone(production);
        return { config, headers: {}, status: 200, statusText: "OK", data: reason ? { code: 409, reason, msg: reason } : { code: 0, data } };
    };
    const host = dom.document.createElement("div");
    dom.document.body.append(host);
    const root = createRoot(host as unknown as HTMLElement);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const render = (revision = "v") =>
        root.render(
            <QueryClientProvider client={client}>
                <MemoryRouter>
                    <AdvertisingProduction key={revision} projectId="ad" userId="owner" confirmedRevisionId={revision} autoEnter />
                </MemoryRouter>
            </QueryClientProvider>,
        );
    const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 60)));
    const button = (text: string) => Array.from(dom.document.querySelectorAll("button")).find((b) => b.textContent?.replaceAll(" ", "") === text)!;
    const input = async (label: string, value: string) =>
        act(() => {
            const el = dom.document.querySelector('[aria-label="' + label + '"]') as any;
            const proto = el.tagName === "TEXTAREA" ? dom.HTMLTextAreaElement.prototype : el.tagName === "SELECT" ? dom.HTMLSelectElement.prototype : dom.HTMLInputElement.prototype;
            Object.getOwnPropertyDescriptor(proto, "value")!.set!.call(el, value);
            el.dispatchEvent(new dom.Event(el.tagName === "SELECT" ? "change" : "input", { bubbles: true }));
        });
    const posts = (suffix: string) => calls.filter((c) => c.method === "post" && c.url.endsWith(suffix));
    try {
        await act(() => render());
        await settle();
        await settle();
        expect(dom.document.querySelector('[aria-label="镜头1视频候选数量"]')).toBeTruthy();
        await input("镜头1视频模型", "m");
        await input("镜头1视频候选数量", "2");
        await act(() => button("获取视频报价").click());
        await settle();
        expect(posts("/batches")).toHaveLength(1);
        expect(posts("/execute")).toHaveLength(0);
        await act(() => button("查看并确认批次").click());
        await settle();
        let checks = Array.from(dom.document.querySelectorAll<HTMLInputElement>('[role="dialog"] input[type="checkbox"]'));
        expect(checks).toHaveLength(2);
        expect(checks.every((c) => !c.checked)).toBe(true);
        expect(button("确认全部费用并生成").disabled).toBe(true);
        await act(() => checks[0].click());
        expect(button("确认全部费用并生成").disabled).toBe(true);
        await act(() => button("暂不生成").click());
        expect(posts("/execute")).toHaveLength(0);
        await act(() => button("查看并确认批次").click());
        await settle();
        checks = Array.from(dom.document.querySelectorAll<HTMLInputElement>('[role="dialog"] input[type="checkbox"]'));
        expect(checks.every((c) => !c.checked)).toBe(true);
        await act(() => {
            checks.forEach((c) => c.click());
        });
        await act(() => button("确认全部费用并生成").click());
        await settle();
        await settle();
        expect(posts("/execute")).toHaveLength(1);
        expect(posts("/execute")[0].body.items).toEqual([
            { submissionId: "b1s0", quoteHash: "hash0" },
            { submissionId: "b1s1", quoteHash: "hash1" },
        ]);
        expect(calls.some((c) => c.method === "get" && c.url.endsWith("/batches/b1"))).toBe(true);
        expect(host.textContent).toContain("待验收");
        expect(production.settings.selections).toEqual({});
        const player = host.querySelector("video");
        expect(player).toBeTruthy();
        expect(host.textContent).toContain("failed");
        batches[0].items[1].task.status = "succeeded";
        await act(async () => {
            await client.invalidateQueries();
        });
        await settle();
        const compareSelect = Array.from(host.querySelectorAll("label"))
            .find((l) => l.textContent?.startsWith("对比视频"))!
            .querySelector("select")!;
        await act(() => {
            compareSelect.value = "b1s1";
            compareSelect.dispatchEvent(new dom.Event("change", { bubbles: true }));
        });
        const secondPlayer = host.querySelectorAll("video")[1];
        expect(secondPlayer).toBeTruthy();
        let paused = 0;
        Object.defineProperty(player!, "duration", { configurable: true, value: 6 });
        Object.defineProperty(secondPlayer!, "duration", { configurable: true, value: 6 });
        Object.defineProperty(player!, "play", {
            configurable: true,
            value: async () => {
                throw new Error("play denied");
            },
        });
        Object.defineProperty(secondPlayer!, "play", { configurable: true, value: async () => {} });
        for (const media of [player!, secondPlayer!])
            Object.defineProperty(media, "pause", {
                configurable: true,
                value: () => {
                    paused++;
                },
            });
        await act(() => button("播放对比视频").click());
        await settle();
        expect(host.textContent).toContain("play denied");
        await input("对比定位秒", "2");
        await act(() => button("定位对比视频").click());
        expect(player!.currentTime).toBe(2);
        expect(secondPlayer!.currentTime).toBe(2);
        expect(paused).toBe(2);
        await act(async () => {
            await client.invalidateQueries();
        });
        await settle();
        expect(host.querySelectorAll("video")[1]).toBe(secondPlayer);
        expect(compareSelect.selectedOptions[0].textContent?.split(" · ")[0]).toBe(host.querySelectorAll("article.ad-candidate h5")[1].textContent?.split(" · ")[0]);
        await act(() => button("验收此候选").click());
        await settle();
        await input("验收理由", "商品清晰");
        holdReview = true;
        await act(() => button("批准并选用").click());
        await settle();
        const editVolume = async (value: string) =>
            act(() => {
                const el = host.querySelector('input[step="0.05"]')!;
                Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, "value")!.set!.call(el, value);
                el.dispatchEvent(new dom.Event("input", { bubbles: true }));
            });
        await editVolume("0.3");
        holdReview = false;
        await act(() => releaseReview!());
        await settle();
        expect(production.settings.selections).toEqual({});
        rejectReview = false;
        await editVolume("0.35");
        await act(() => new Promise((r) => setTimeout(r, 800)));
        await settle();
        expect(posts("/reviews")).toHaveLength(1);
        expect(production.settings.musicVolume).toBe(0.35);
        expect(production.settings.selections).toEqual({});
        holdReview = true;
        await act(() => button("批准并选用").click());
        await settle();
        await editVolume("0.4");
        holdReview = false;
        await act(() => releaseReview!());
        await settle();
        await settle();
        expect(production.settings.selections).toEqual({});
        await act(() => new Promise((r) => setTimeout(r, 800)));
        await settle();
        expect(posts("/reviews")).toHaveLength(2); // Ordinary late music edits never authorize the missing review.
        await act(() => button("重试原验收提交").click());
        await settle();
        await settle();
        expect(posts("/reviews")[2].body).toEqual(posts("/reviews")[1].body);
        expect(production.settings.selections.shot.videoSubmissionId).toBe("b1s0");
        expect(production.settings.musicVolume).toBe(0.4);
        await act(() => {
            dom.document.querySelector<HTMLButtonElement>('[role="dialog"] .ant-modal-close')?.click();
        });
        await settle();
        expect(host.querySelector("video")).toBe(player);
        await act(async () => {
            await client.invalidateQueries();
        });
        await settle();
        expect(host.querySelector("video")).toBe(player);
        activity = [{ id: "offpage", productionId: "p", shotId: "shot", kind: "video", publicRequest: { modelId: "m", prompt: "original" }, task: { id: "unknown", type: "video_test", status: "failed" } }];
        await act(async () => {
            await client.invalidateQueries();
        });
        await settle();
        expect(button("获取视频报价").disabled).toBe(true);
        activity = [];
        await act(async () => {
            await client.invalidateQueries();
        });
        await settle();
        expect(posts("/batches")).toHaveLength(1);
        await act(() => button("记录问题").click());
        await settle();
        await input("问题说明", "标志变形");
        await act(() => button("保存问题").click());
        await settle();
        expect(issues).toHaveLength(0);
        await act(() => button("重试原问题提交").click());
        await settle();
        expect(posts("/issues")[1].body).toEqual(posts("/issues")[0].body);
        expect(issues).toHaveLength(1);
        expect(posts("/batches")).toHaveLength(1);
        expect(posts("/execute")).toHaveLength(1);
        await act(() => {
            const close = dom.document.querySelector<HTMLButtonElement>('[role="dialog"] .ant-modal-close');
            close?.click();
        });
        await settle();
        await act(() => button("审计与问题").click());
        await settle();
        await act(() => button("以此问题准备返工").click());
        await settle();
        expect(host.textContent).toContain("来源问题");
        expect(posts("/batches")).toHaveLength(1);
        expect(issues[0].status).toBe("open");
        losePrepareBeforeAdmission = true;
        await act(() => button("获取视频报价").click());
        await settle();
        expect(posts("/batches")).toHaveLength(2);
        await act(() => button("重试原报价请求").click());
        await settle();
        expect(posts("/batches")[2].body).toEqual(posts("/batches")[1].body);
        expect(posts("/batches")[1].body.adjustment).toContain("标志变形");
        await act(() => button("查看并确认批次").click());
        await settle();
        checks = Array.from(dom.document.querySelectorAll<HTMLInputElement>('[role="dialog"] input[type="checkbox"]'));
        await act(() => checks.forEach((c) => c.click()));
        await input("镜头1视频候选数量", "3");
        expect(button("确认全部费用并生成")?.disabled ?? true).toBe(true);
        await act(() => render("v2"));
        await settle();
        expect(dom.document.querySelector('[role="dialog"]')).toBeNull();
        expect(posts("/execute")).toHaveLength(1);
    } finally {
        await act(() => root.unmount());
        client.clear();
        apiClient.defaults.adapter = original;
        await dom.happyDOM.close();
    }
}, 65000);

test("candidate display identity survives approval, batches, order and remount", async () => {
    if (!process.env.AD_LABEL_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path, "-t", "candidate display identity"], { env: { ...process.env, AD_LABEL_CHECK: "1" }, encoding: "utf8", timeout: 20000 });
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
    const { setActiveUserScope } = await import("../src/lib/user-scope");
    setActiveUserScope("label-check");
    const candidate = (id: string, batchId: string, executed: boolean): any => ({
        id,
        batchId,
        candidateIndex: 1,
        productionId: "p",
        shotId: "shot",
        kind: "video",
        publicRequest: { modelId: "m", prompt: id },
        quote: { quoteHash: id, amountMicrocredits: 1, expiresAt: new Date(Date.now() + 60000).toISOString() },
        ...(executed ? { task: { id: "task" + id, status: "succeeded", resultJson: JSON.stringify({ video: { storageKey: "resource:" + id } }) } } : {}),
    });
    const old = candidate("11111111-1111-4111-8111-111111111111", "batch-old", true);
    const next = candidate("22222222-2222-4222-8222-222222222222", "batch-next", false);
    const batch: any = { batch: { id: "batch-next", shotId: "shot", kind: "video", count: 1, adjustment: "", clientOperationId: crypto.randomUUID() }, items: [next] };
    let showHistory = true;
    let executeCount = 0;
    const original = apiClient.defaults.adapter;
    apiClient.defaults.adapter = async (config) => {
        const url = config.url || "";
        let data: any;
        if (url.endsWith("/activity")) data = { items: [], batches: [] };
        else if (url.endsWith("/execute")) {
            executeCount++;
            next.task = candidate(next.id, next.batchId, true).task;
            batch.batch.approvedAt = new Date().toISOString();
            data = batch;
        } else if (url.endsWith("/batches")) data = { items: showHistory ? [batch] : [], total: 21 };
        else if (url.endsWith("/reviews")) data = { items: [], total: 0 };
        else if (url.includes("/resources/")) data = { resource: { id: url.split("/").at(-1), status: "ready", mimeType: "video/mp4", durationMs: 6000 } };
        else throw new Error("Unexpected request " + url);
        return { config, headers: {}, status: 200, statusText: "OK", data: { code: 0, data } };
    };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const host = dom.document.createElement("div");
    dom.document.body.append(host);
    const root = createRoot(host as unknown as HTMLElement);
    const props: any = {
        shot: { id: "shot", durationMs: 5000, visual: "product", voiceover: "", onScreenText: "" },
        index: 0,
        models: [],
        protocols: [],
        submissions: [old],
        production: { production: { id: "p" }, settings: { selections: {}, ttsModelId: "", voiceId: "" }, confirmedSnapshot: { snapshot: { draft: { productResourceIds: [], shots: [{ id: "shot", durationMs: 5000 }] } } } },
        projectId: "ad",
        disabled: false,
        current: () => true,
        flush: async () => {},
        review: async () => {},
    };
    let mount = 0;
    const render = () =>
        root.render(
            <QueryClientProvider client={client}>
                <AdvertisingShotGeneration key={mount} {...props} />
            </QueryClientProvider>,
        );
    const settle = () => act(() => new Promise((r) => setTimeout(r, 80)));
    const button = (text: string) => Array.from(dom.document.querySelectorAll("button")).find((b) => b.textContent?.replaceAll(" ", "") === text)!;
    const label = (id: string) =>
        Array.from(host.querySelectorAll("article.ad-candidate"))
            .find((c) => c.textContent?.includes("候选 " + id))!
            .querySelector("h5")!
            .textContent!.split(" · ")[0];
    try {
        await act(render);
        await settle();
        await settle();
        const oldLabel = label(old.id);
        const oldPlayer = host.querySelector("video");
        await act(() => button("查看并确认批次").click());
        await settle();
        const approvedLabel = dom.document.querySelector("[role=dialog] .ad-check")!.textContent!.match(/批准 (.+)：/)![1];
        await act(() => dom.document.querySelector<HTMLInputElement>("[role=dialog] input[type=checkbox]")!.click());
        await act(() => button("确认全部费用并生成").click());
        await settle();
        await settle();
        expect(executeCount).toBe(1);
        expect(host.querySelector("video")).toBe(oldPlayer);
        expect(label(next.id)).toBe(approvedLabel);
        expect(label(old.id)).toBe(oldLabel);
        expect(approvedLabel).not.toBe(oldLabel);
        props.submissions = [next, old];
        await act(render);
        await settle();
        expect(label(next.id)).toBe(approvedLabel);
        await act(() => host.querySelector<HTMLButtonElement>('[aria-label="下一页"]')!.click());
        await settle();
        expect(label(next.id)).toBe(approvedLabel);
        await act(() => host.querySelector<HTMLButtonElement>('[aria-label="上一页"]')!.click());
        await settle();
        expect(label(next.id)).toBe(approvedLabel);
        showHistory = false;
        props.submissions = [old];
        await act(async () => {
            render();
            await client.invalidateQueries();
        });
        await settle();
        showHistory = true;
        props.submissions = [next, old];
        mount++;
        await act(async () => {
            render();
            await client.invalidateQueries();
        });
        await settle();
        await settle();
        expect(label(next.id)).toBe(approvedLabel);
        expect(label(old.id)).toBe(oldLabel);
        const options = Array.from(host.querySelectorAll("option")).filter((o) => o.value === next.id);
        expect(options.length).toBeGreaterThan(0);
        expect(options.every((o) => o.textContent?.startsWith(approvedLabel))).toBe(true);
    } finally {
        await act(() => root.unmount());
        client.clear();
        apiClient.defaults.adapter = original;
        await dom.happyDOM.close();
    }
}, 25000);
