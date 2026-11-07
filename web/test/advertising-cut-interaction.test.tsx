import { expect, test } from "bun:test";
import { Window } from "happy-dom";
import { spawnSync } from "node:child_process";
test("cut consumer pins review and delivery to same existing output", async () => {
    if (!process.env.AD_CUT_DOM) {
        const r = spawnSync(process.execPath, ["test", import.meta.path], { env: { ...process.env, AD_CUT_DOM: "1" }, encoding: "utf8", timeout: 60000 });
        expect(r.stderr + r.stdout).not.toContain("(fail)");
        expect(r.status).toBe(0);
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
    const { apiClient } = await import("../src/services/api/request");
    const { setActiveUserScope } = await import("../src/lib/user-scope");
    const { useUserStore } = await import("../src/stores/use-user-store");
    const { emptyAdvertisingDraft } = await import("../src/services/api/advertising-project");
    setActiveUserScope("cut-owner");
    useUserStore.setState({ user: { id: "cut-owner" } as any });
    const draft = emptyAdvertisingDraft();
    draft.shots = [{ id: "shot", durationMs: 5000, visual: "TEST", voiceover: "", onScreenText: "", referenceResourceIds: [] }];
    const settings = { ttsModelId: "", voiceId: "", selections: { shot: { videoSubmissionId: "s", audioSubmissionId: "", videoStartMs: 100 } }, musicResourceId: "", musicVolume: 0.15, fit: "pad" };
    const production = { production: { id: "p", revision: 3, confirmedRevisionId: "v" }, settings, confirmedSnapshot: { id: "v", sourceRevision: 1, snapshot: { name: "TEST", draft, resources: [] } } };
    const assembly = {
        id: "a",
        productionId: "p",
        sourceProductionRevision: 2,
        selectionHash: "legacy",
        sourceFingerprint: "actual",
        current: true,
        sourceSettings: {
            selections: {
                "10000000-0000-4000-8000-000000000002": { videoSubmissionId: "candidate-second", audioSubmissionId: "", videoStartMs: 200 },
                "f0000000-0000-4000-8000-000000000001": { videoSubmissionId: "candidate-first", audioSubmissionId: "voice-first", videoStartMs: 100 },
            },
            musicResourceId: "music",
            musicVolume: 0.15,
            fit: "pad",
        },
        sources: [
            { key: "candidate-first", taskId: "source-task", resourceId: "source", durationMs: 6000, width: 640, height: 360 },
            { key: "voice-first", taskId: "voice-task", resourceId: "voice", durationMs: 1000, width: 0, height: 0 },
            { key: "music", resourceId: "music", durationMs: 10000, width: 0, height: 0 },
            { key: "logo", resourceId: "logo", durationMs: 0, width: 128, height: 128 },
        ],
        timeline: {
            version: 2,
            tracks: [],
            clips: [
                { id: "video-10000000-0000-4000-8000-000000000002", kind: "video", startMs: 5000, durationMs: 5000 },
                { id: "logo", kind: "image", startMs: 0, durationMs: 10000, directMedia: { storageKey: "resource:logo" } },
                { id: "video-f0000000-0000-4000-8000-000000000001", kind: "video", startMs: 0, durationMs: 5000 },
            ],
            durationMs: 10000,
            output: { width: 1080, height: 1920, fit: "pad", burnSubtitles: true },
        },
    };
    const render = { id: "render", assemblyId: "a", taskId: "render-task", task: { id: "render-task", status: "succeeded" }, resourceId: "output", available: true };
    const reviews: any[] = [];
    let renderPosts = 0;
    let assemblyPosts = 0;
    let missing = false;
    let issuePosts: any[] = [];
    let issueFail = true;
    let issueSaved: any;
    let loseDelivery = true;
    let failReviewRead = true;
    const reviewPosts: any[] = [];
    const original = apiClient.defaults.adapter;
    apiClient.defaults.adapter = async (config) => {
        const url = config.url || "";
        const body = config.data ? JSON.parse(config.data) : {};
        let data: any = {};
        if (url === "/resources/access") data = { items: body.map((item: any) => ({ access: { url: "http://localhost/fixture-" + item.resourceId, expiresAt: new Date(Date.now() + 60000).toISOString() } })) };
        else if (url === "/model-catalog") data = { channels: [] };
        else if (url === "/plugins/catalog") data = { providers: [] };
        else if (url.endsWith("/issues") && config.method === "post") {
            issuePosts.push(body);
            if (issueFail) throw new Error("issue response lost");
            issueSaved = body;
            data = body;
        } else if (url.endsWith("/issues") && config.params?.clientOperationId) {
            if (!issueSaved) return { data: { code: 404, reason: "not_found", msg: "missing" }, status: 404, statusText: "Not Found", headers: {}, config };
            data = issueSaved;
        } else if (url.endsWith("/reviews") && config.method === "post") {
            reviewPosts.push(body);
            expect(body.assemblyId).toBe("a");
            expect(body.renderAttemptId).toBe("render");
            expect(body.resourceId).toBe("output");
            if (body.checkpoint === "delivery") expect(body.acceptedReviewId).toBe("review-assembly");
            const review = { ...body, id: "review-" + body.checkpoint };
            reviews.unshift(review);
            if (body.checkpoint === "assembly") (assembly as any).latestReview = review;
            data = { review, production };
            if (body.checkpoint === "delivery" && loseDelivery) {
                loseDelivery = false;
                throw new Error("delivery response lost");
            }
        } else if (url.endsWith("/reviews")) {
            if (config.params?.clientOperationId && failReviewRead && reviews[0]?.checkpoint === "delivery") {
                failReviewRead = false;
                throw new Error("temporary read failure");
            }
            data = config.params?.clientOperationId ? { review: reviews.find((r) => r.clientOperationId === config.params!.clientOperationId), production } : { items: reviews, total: reviews.length };
        } else if (url.endsWith("/assemblies/a/renders")) {
            if (config.method === "post") renderPosts++;
            data = { items: [{ ...render, available: !missing }], total: 1 };
        } else if (url.endsWith("/assemblies/a")) data = assembly;
        else if (url.endsWith("/assemblies")) {
            if (config.method === "post") assemblyPosts++;
            data = { items: [assembly], total: 1 };
        } else if (url.endsWith("/productions") || url.endsWith("/productions/p")) data = production;
        else data = { items: [], batches: [], total: 0 };
        return { data: { code: 0, data }, status: 200, statusText: "OK", headers: {}, config };
    };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    const show = (stage: "preview" | "export") =>
        root.render(
            <QueryClientProvider client={client}>
                <MemoryRouter>
                    <AdvertisingProduction projectId="ad_cut" userId="cut-owner" confirmedRevisionId="v" autoEnter stage={stage} />
                </MemoryRouter>
            </QueryClientProvider>,
        );
    const settle = async () => {
        for (let i = 0; i < 6; i++) await act(() => new Promise((r) => setTimeout(r, 20)));
    };
    const button = (text: string) => Array.from(host.querySelectorAll("button")).find((b) => b.textContent === text)!;
    try {
        await act(() => show("preview"));
        await settle();
        expect(renderPosts).toBe(0);
        expect(assemblyPosts).toBe(0);
        expect(host.textContent).toContain("640 × 360");
        const frozenShotRows = () =>
            Array.from(host.querySelectorAll(".ad-cut-review li"))
                .map((li) => li.textContent)
                .filter((text) => text.startsWith("镜头 "));
        expect(frozenShotRows()).toEqual(["镜头 1：视频 C-candidate-first · 裁剪起点 100ms · 旁白 C-voice-first", "镜头 2：视频 C-candidate-second · 裁剪起点 200ms · 无旁白，不需要 TTS"]);
        expect(host.querySelector(".ad-cut-review")?.textContent).toContain("背景音乐 ·");
        expect(host.querySelector(".ad-cut-review")?.textContent).toContain("Logo ·");
        expect(host.querySelector('[aria-label="镜头1裁剪起点（毫秒）"]')).not.toBeNull();
        await act(() => button("审看此输出").click());
        await settle();
        const video = host.querySelector('video[aria-label="成片MP4"]');
        expect(video).not.toBeNull();
        await act(() => {
            const field = host.querySelector('[aria-label="成片验收理由"]')!;
            Object.getOwnPropertyDescriptor(dom.HTMLTextAreaElement.prototype, "value")!.set!.call(field, "watched exact file");
            field.dispatchEvent(new dom.Event("input", { bubbles: true }));
        });
        await act(() => button("批准此成片").click());
        await settle();
        expect(reviews[0].checkpoint).toBe("assembly");
        await act(() => show("export"));
        await settle();
        expect(host.querySelector('video[aria-label="成片MP4"]')).toBe(video);
        expect(frozenShotRows()[0]).toContain("C-candidate-first · 裁剪起点 100ms · 旁白 C-voice-first");
        expect(renderPosts).toBe(0);
        await act(() => button("确认交付此输出").click());
        await settle();
        expect(reviews[0].checkpoint).toBe("delivery");
        expect(host.textContent).toContain("原验收待核对");
        await act(() => button("查询原验收").click());
        await settle();
        expect(reviewPosts.filter((r) => r.checkpoint === "delivery")).toHaveLength(1);
        expect(host.textContent).not.toContain("原验收待核对");
        expect(renderPosts).toBe(0);
        expect(assemblyPosts).toBe(0);
        missing = true;
        await act(() => button("重新检查输出可用性").click());
        await settle();
        expect(host.textContent).toContain("输出当前不可用");
        expect(host.querySelector('video[aria-label="成片MP4"]')).toBe(video);
        expect(frozenShotRows()[0]).toContain("C-candidate-first · 裁剪起点 100ms · 旁白 C-voice-first");
        missing = false;
        render.id = "render-two";
        render.resourceId = "output-two";
        await act(() => button("重新检查输出可用性").click());
        await settle();
        await act(() => button("审看此输出").click());
        await settle();
        expect(host.textContent).not.toContain("交付确认已保存（同一输出）");
        await act(() => {
            const field = host.querySelector('[aria-label="成片问题"]')!;
            Object.getOwnPropertyDescriptor(dom.HTMLTextAreaElement.prototype, "value")!.set!.call(field, "check original output");
            field.dispatchEvent(new dom.Event("input", { bubbles: true }));
        });
        await act(() => {
            button("记录此输出问题").click();
            button("记录此输出问题").click();
        });
        await settle();
        expect(issuePosts).toHaveLength(1);
        await act(() => button("查询原成片问题").click());
        await settle();
        expect(issuePosts).toHaveLength(1);
        issueFail = false;
        await act(() => button("重试原成片问题").click());
        await settle();
        expect(issuePosts).toHaveLength(2);
        expect(issuePosts[1]).toEqual(issuePosts[0]);
        expect(issuePosts[1].renderAttemptId).toBe("render-two");
    } finally {
        await act(() => root.unmount());
        client.clear();
        apiClient.defaults.adapter = original;
        await dom.happyDOM.abort();
    }
});
test("cut cache rejects invalid fit volume and trims", async () => {
    const { validCachedSettings } = await import("../src/pages/ad-projects/advertising-production");
    const valid = { fit: "pad", ttsModelId: "", voiceId: "", musicResourceId: "", musicVolume: 0.15, selections: { s: { videoSubmissionId: "v", audioSubmissionId: "", videoStartMs: 0 } } };
    expect(validCachedSettings(valid)).toBe(true);
    for (const patch of [
        { fit: "auto" },
        { musicVolume: 2 },
        { musicVolume: -1 },
        { selections: { s: { videoSubmissionId: "v", audioSubmissionId: "", videoStartMs: 0.5 } } },
        { selections: { s: { videoSubmissionId: "", audioSubmissionId: "", videoStartMs: 1 } } },
    ])
        expect(validCachedSettings({ ...valid, ...patch })).toBe(false);
});
