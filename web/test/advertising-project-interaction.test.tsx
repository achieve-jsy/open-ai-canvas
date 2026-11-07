import { expect, test, mock } from "bun:test";
import { Window } from "happy-dom";
import { spawnSync } from "node:child_process";

test("advertising form edits stable shots and explicit seconds without discarding fields", async () => {
    if (!process.env.ADVERTISING_DOM_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path, "-t", "advertising form edits"], { env: { ...process.env, ADVERTISING_DOM_CHECK: "1" }, encoding: "utf8", timeout: 40000 });
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
    const { AdvertisingDraftForm } = await import("../src/pages/ad-projects/advertising-draft-form");
    const { emptyAdvertisingDraft } = await import("../src/services/api/advertising-project");
    const host = dom.document.createElement("div");
    dom.document.body.append(host);
    const root = createRoot(host as unknown as HTMLElement);
    let value = emptyAdvertisingDraft();
    value.brief.cta = "立即选购";
    const render = () =>
        root.render(
            <AdvertisingDraftForm
                value={value}
                onChange={(next) => {
                    value = next;
                    render();
                }}
            />,
        );
    try {
        await act(render);
        const scene = host.querySelector<HTMLTextAreaElement>('[aria-label="广告场景"]')!;
        expect(scene).toBeTruthy();
        expect(host.querySelector<HTMLDetailsElement>("details")?.open).toBe(false);
        await act(() => {
            Object.getOwnPropertyDescriptor(dom.HTMLTextAreaElement.prototype, "value")!.set!.call(scene, "雨天通勤，突出轻巧便携");
            scene.dispatchEvent(new dom.Event("input", { bubbles: true }));
        });
        expect(value.brief.description).toBe("雨天通勤，突出轻巧便携");
        const goal = host.querySelector<HTMLSelectElement>('[aria-label="广告目标"]')!;
        expect(goal).toBeTruthy();
        await act(() => {
            goal.value = "awareness";
            goal.dispatchEvent(new dom.Event("change", { bubbles: true }));
        });
        expect(value.brief.goal).toBe("awareness");
        expect(value.brief.cta).toBe("");
        expect(host.textContent).not.toContain("行动号召（CTA）");
        await act(() => host.querySelector("summary")!.click());
        await act(() => {
            goal.value = "conversion";
            goal.dispatchEvent(new dom.Event("change", { bubbles: true }));
        });
        expect(host.textContent).toContain("行动号召（CTA）");
        expect(host.querySelector<HTMLDetailsElement>("details")?.open).toBe(true);
        expect(host.querySelector<HTMLInputElement>('[aria-label="商品名称"]')).toBeTruthy();
        const button = (text: string) => Array.from(host.querySelectorAll("button")).find((b) => b.textContent === text)!;
        for (let i = 0; i < 3; i++) await act(() => button("添加镜头").click());
        expect(value.shots).toHaveLength(3);
        const ids = value.shots.map((s) => s.id);
        expect(new Set(ids).size).toBe(3);
        await act(() => host.querySelector<HTMLButtonElement>('[aria-label="镜头2上移"]')!.click());
        expect(value.shots.map((s) => s.id)).toEqual([ids[1], ids[0], ids[2]]);
        const input = host.querySelector('[aria-label="镜头1时长（秒）"]')!;
        await act(() => {
            Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, "value")!.set!.call(input, "6.5");
            input.dispatchEvent(new dom.Event("input", { bubbles: true }));
        });
        expect(value.shots[0].durationMs).toBe(6500);
        expect(value.shots[0].id).toBe(ids[1]);

        const { apiClient } = await import("../src/services/api/request");
        const { useUserStore } = await import("../src/stores/use-user-store");
        const { setActiveUserScope } = await import("../src/lib/user-scope");
        let uploadResult: any = { storageKey: "local:test", pendingRemoteUpload: true, mimeType: "image/png" };
        let finishUpload: ((v: any) => void) | undefined;
        const fileStorage = await import("../src/services/file-storage");
        mock.module("../src/services/file-storage", () => ({
            ...fileStorage,
            uploadMediaFile: async () =>
                uploadResult === "wait"
                    ? new Promise((resolve) => {
                          finishUpload = resolve;
                      })
                    : uploadResult,
        }));
        const { default: Page } = await import("../src/pages/ad-projects/detail");
        const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
        const { createMemoryRouter, RouterProvider } = await import("react-router");
        useUserStore.setState({ user: { id: "owner" } as any, hydrated: true });
        setActiveUserScope("owner");
        let server: any = { project: { id: "ad", name: "Ad" }, draft: emptyAdvertisingDraft(), revision: 1, resources: [] };
        let failSave = "";
        let finishSave: (() => void) | undefined;
        let deferSave = false;
        let confirmations = 0;
        let productionPosts = 0;
        let failProduction = true;
        let deferredProductionSave: (() => void) | undefined;
        let oldConfirmation = false;
        let failReadAfterConfirm = false;
        let plan: any;
        let planPosts = 0;
        let timeoutPost = false;
        let missingPlanGets = 0;
        let missingLatest = false;
        let planGets = 0;
        const savedRevisions: number[] = [];
        const original = apiClient.defaults.adapter;
        apiClient.defaults.adapter = async (config) => {
            let data: any;
            const url = config.url!;
            const req = config.data ? JSON.parse(String(config.data)) : undefined;
            if (url.endsWith("/script-plans") && config.method === "post") {
                planPosts++;
                expect(req).toEqual({ expectedRevision: server.revision, clientOperationId: expect.any(String) });
                plan = { operationId: req.clientOperationId, sourceRevision: server.revision, status: "running" };
                data = plan;
                if (timeoutPost) {
                    plan = { ...plan, status: "ready", draft: structuredClone(server.draft) };
                    throw Object.assign(new Error("proxy unavailable"), { isAxiosError: true, response: { status: 503, data: { code: 503, msg: "unavailable" } } });
                }
            } else if (url.endsWith("/script-plans")) {
                data = missingLatest ? null : (plan ?? null);
            } else if (url.includes("/script-plans/")) {
                planGets++;
                if (missingPlanGets-- > 0) return { config, headers: {}, status: 404, statusText: "Not Found", data: { code: 404, msg: "not found" } };
                data = plan;
            } else if (url.endsWith("/productions")) {
                productionPosts++;
                if (failProduction) throw new Error("create production unavailable");
                data = {
                    production: { id: "p", revision: 1, confirmedRevisionId: "v1" },
                    settings: { ttsModelId: "", voiceId: "", selections: {}, musicResourceId: "", musicVolume: 0.15 },
                    confirmedSnapshot: { id: "v1", sourceRevision: 8, snapshot: { name: server.project.name, draft: server.draft, resources: [] } },
                };
            } else if (url.includes("/productions/") && config.method === "patch") {
                await new Promise<void>((resolve) => {
                    deferredProductionSave = resolve;
                });
                throw new Error("settings offline");
            } else if (url.includes("/productions/")) data = { items: [], total: 0, page: 1, pageSize: 20 };
            else if (url === "/model-catalog") data = { channels: [] };
            else if (url === "/plugins/catalog") data = { providers: [] };
            else if (url.endsWith("/confirm") && oldConfirmation) {
                data = { currentRevision: 20, snapshot: { id: "v1", sourceRevision: 8, snapshot: { name: "Old", draft: emptyAdvertisingDraft(), resources: [] } } };
            } else if (url.endsWith("/confirm")) {
                confirmations++;
                failReadAfterConfirm = true;
                server = { ...server, revision: server.revision + 1, latestConfirmedRevisionId: "v1" };
                data = { currentRevision: server.revision, snapshot: { id: "v1", projectId: "ad", sourceRevision: server.revision - 1, snapshot: { name: server.project.name, draft: server.draft, resources: [] } } };
            } else if (config.method === "put") {
                if (deferSave)
                    await new Promise<void>((resolve) => {
                        finishSave = resolve;
                    });
                if (failSave) return { config, headers: {}, status: 200, statusText: "OK", data: { code: 409, msg: "保存失败", reason: failSave } };
                savedRevisions.push(req.expectedRevision);
                server = { ...server, project: { ...server.project, name: req.name }, draft: req.draft, revision: server.revision + 1 };
                data = structuredClone(server);
            } else if (url.endsWith("/revisions")) data = { items: [], total: 0, page: 1, pageSize: 20 };
            else if (url.includes("/revisions/")) data = { id: "v1", sourceRevision: server.revision - 1, snapshot: { name: server.project.name, draft: server.draft, resources: [] } };
            else if (failReadAfterConfirm) {
                failReadAfterConfirm = false;
                throw new Error("read after confirm offline");
            } else data = { ...structuredClone(server), project: { ...server.project, id: url.endsWith("/other") ? "other" : "ad" } };
            return { config, headers: {}, status: 200, statusText: "OK", data: { code: 0, data } };
        };
        const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
        const router = createMemoryRouter([{ path: "/ad-projects/:id", element: <Page /> }], { initialEntries: ["/ad-projects/ad"] });
        const settle = async () => {
            await act(() => new Promise((resolve) => setTimeout(resolve, 30)));
        };
        const pageButton = (text: string) => Array.from(dom.document.querySelectorAll("button")).find((b) => b.textContent?.replaceAll(" ", "") === text)!;
        const inputValue = async (label: string, next: string) => {
            const el = host.querySelector('[aria-label="' + label + '"]')!;
            await act(() => {
                Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, "value")!.set!.call(el, next);
                el.dispatchEvent(new dom.Event("input", { bubbles: true }));
            });
        };
        try {
            const { localForageStorageForScope } = await import("../src/lib/localforage-storage");
            const storage = localForageStorageForScope("owner");
            const good = { name: "Ad", draft: emptyAdvertisingDraft() };
            for (const cached of [
                { base: { revision: 1, value: good }, value: { draft: {} } },
                { base: { revision: 1, value: { draft: {} } }, value: good },
                { base: { revision: 1, value: good }, value: { ...good, draft: { ...good.draft, shots: [{ id: "bad" }] } } },
                { base: { revision: 1, value: good }, value: good, conflict: { remote: { revision: 2, value: { draft: {} } } } },
            ]) {
                await storage.setItem("advertising-autosave:owner:ad:draft", JSON.stringify(cached));
                await act(() =>
                    root.render(
                        <QueryClientProvider client={client}>
                            <RouterProvider router={router} />
                        </QueryClientProvider>,
                    ),
                );
                await settle();
                expect(host.querySelector<HTMLInputElement>('[aria-label="项目名称"]')?.value).toBe("Ad");
                expect(host.textContent).not.toContain("已与服务端同步");
                expect(savedRevisions).toHaveLength(0);
                await act(() => root.render(<div />));
            }
            await storage.removeItem!("advertising-autosave:owner:ad:draft");
            await act(() =>
                root.render(
                    <QueryClientProvider client={client}>
                        <RouterProvider router={router} />
                    </QueryClientProvider>,
                ),
            );
            await settle();
            const stages = host.querySelector('[aria-label="广告制作阶段"]');
            expect(stages).toBeTruthy();
            expect(stages!.querySelectorAll("button")).toHaveLength(5);
            await act(() => stages!.querySelectorAll<HTMLButtonElement>("button")[1].click());
            expect(stages!.querySelector('[aria-current="step"]')?.textContent).toContain("脚本确认");
            expect(confirmations).toBe(0);
            expect(planPosts).toBe(0);
            await act(() => stages!.querySelectorAll<HTMLButtonElement>("button")[0].click());
            await inputValue("项目名称", "Edited ad");
            await act(() => pageButton("保存草稿").click());
            await settle();
            expect(savedRevisions).toEqual([1]);
            deferSave = true;
            await inputValue("商品名称", "A");
            await act(() => new Promise((resolve) => setTimeout(resolve, 760)));
            expect(finishSave).toBeDefined();
            expect(host.querySelector<HTMLInputElement>('[aria-label="商品名称"]')!.disabled).toBe(false);
            await inputValue("商品名称", "B");
            deferSave = false;
            await act(() => finishSave!());
            await settle();
            expect(server.draft.brief.productName).toBe("B");
            expect(planPosts).toBe(0);
            await inputValue("商品名称", "我的商品");
            expect(pageButton("确认脚本").disabled).toBe(true);
            failSave = "internal";
            await act(() => pageButton("保存草稿").click());
            await settle();
            expect((host.querySelector('[aria-label="商品名称"]') as any).value).toBe("我的商品");
            failSave = "advertising_revision_conflict";
            server = { ...server, revision: 7, project: { ...server.project, name: "远端更新" } };
            await act(() => pageButton("保存草稿").click());
            await settle();
            expect(host.textContent).toContain("服务端修订 7");
            expect((host.querySelector('[aria-label="商品名称"]') as any).value).toBe("我的商品");
            await act(() => pageButton("保留本地内容继续编辑").click());
            failSave = "";
            await act(() => pageButton("保存草稿").click());
            await settle();
            expect(savedRevisions.at(-1)).toBe(7);
            await act(() => pageButton("确认脚本").click());
            await settle();
            await act(() => pageButton("确认并冻结").click());
            await settle();
            expect(confirmations).toBe(1);
            expect(host.textContent).toContain("修订 9");
            expect(host.querySelector('[aria-current="step"]')?.textContent).toContain("镜头制作");
            expect(host.textContent).toContain("create production unavailable");
            expect(productionPosts).toBe(1);
            failProduction = false;
            await act(() => pageButton("重新连接服务端制作").click());
            await settle();
            await settle();
            expect(productionPosts).toBe(2);
            expect(confirmations).toBe(1);
            expect(planPosts).toBe(0);
            const stageButton = (stage: string) => host.querySelector<HTMLButtonElement>('[aria-label="广告制作阶段"] [aria-label="' + stage + '"]')!;
            await act(() => stageButton("成片预览").click());
            const volume = host.querySelector<HTMLInputElement>('input[step="0.05"]')!;
            await act(() => {
                Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, "value")!.set!.call(volume, "0.35");
                volume.dispatchEvent(new dom.Event("input", { bubbles: true }));
            });
            await act(() => stageButton("导出交付").click());
            await act(() => stageButton("成片预览").click());
            expect(host.querySelector('input[step="0.05"]')).toBe(volume);
            expect(volume.value).toBe("0.35");
            await act(() => pageButton("保存制作设置").click());
            await settle();
            expect(deferredProductionSave).toBeDefined();
            await act(() => router.navigate("/ad-projects/other"));
            await settle();
            expect(router.state.location.pathname).toBe("/ad-projects/ad");
            expect(dom.document.body.textContent).toContain("离开广告编辑");
            await act(() => pageButton("继续编辑").click());
            await act(() => deferredProductionSave!());
            await settle();
            await act(() => stageButton("素材与需求").click());
            // Refetch must not replace active edits; remount must recover saved backend state.
            await inputValue("商品名称", "未保存");
            await act(async () => {
                await client.invalidateQueries({ queryKey: ["advertising", "owner", "ad"] });
            });
            await settle();
            expect((host.querySelector('[aria-label="商品名称"]') as any).value).toBe("未保存");
            server = {
                ...server,
                revision: 10,
                draft: { ...server.draft, brief: { ...server.draft.brief, productName: "Updated while away", description: "清晨通勤" }, productResourceIds: ["product-1"] },
                resources: [{ id: "product-1", title: "商品图", mimeType: "image/png", available: true }],
            };
            await act(() => root.render(<div />));
            await act(() =>
                root.render(
                    <QueryClientProvider client={client}>
                        <RouterProvider router={router} />
                    </QueryClientProvider>,
                ),
            );
            await settle();
            expect((host.querySelector('[aria-label="商品名称"]') as any).value).toBe("未保存");
            expect(pageButton("生成脚本").disabled).toBe(true);
            await act(() => pageButton("重新读取服务端版本").click());
            await settle();
            const originalConfirm = window.confirm;
            window.confirm = () => true;
            await act(() => pageButton("采用服务端版本").click());
            window.confirm = originalConfirm;
            await settle();
            expect((host.querySelector('[aria-label="商品名称"]') as any).value).toBe("Updated while away");
            expect(pageButton("生成脚本").disabled).toBe(false);
            await act(() => pageButton("生成脚本").click());
            await settle();
            expect(planPosts).toBe(1);
            expect(dom.localStorage.getItem("ad-script-plan:ad:user:owner")).toBe(plan.operationId);
            await act(() => root.render(<div />));
            await act(() =>
                root.render(
                    <QueryClientProvider client={client}>
                        <RouterProvider router={router} />
                    </QueryClientProvider>,
                ),
            );
            await settle();
            expect(planPosts).toBe(1);
            plan = {
                ...plan,
                status: "ready",
                draft: {
                    ...server.draft,
                    shots: [
                        { id: "shot-a", durationMs: 5000, visual: "地铁通勤特写", voiceover: "随时轻装出发", onScreenText: "随行", referenceResourceIds: ["product-1"] },
                        { id: "shot-b", durationMs: 10000, visual: "户外行走", voiceover: "轻快每一天", onScreenText: "立即出发", referenceResourceIds: [] },
                    ],
                },
                evidence: [{ claim: "轻巧", source: "商品图", resourceId: "product-1" }],
                uncertainties: ["续航尚待核实"],
            };
            await act(() => new Promise((resolve) => setTimeout(resolve, 1700)));
            expect(host.textContent).toContain("地铁通勤特写");
            expect(host.textContent).toContain("户外行走");
            expect(host.textContent).toContain("续航尚待核实");
            expect(host.textContent).toContain("轻巧");
            expect(server.draft.shots).toHaveLength(0);
            // Another browser has no local operation ID but must recover the saved candidate.
            dom.localStorage.removeItem("ad-script-plan:ad:user:owner");
            await act(() => router.navigate("/ad-projects/other"));
            await settle();
            await act(() => router.navigate("/ad-projects/ad"));
            await settle();
            expect(host.textContent).toContain("地铁通勤特写");
            expect(pageButton("采用候选脚本").disabled).toBe(false);
            expect(planPosts).toBe(1);
            await inputValue("商品名称", "正在编辑");
            expect(pageButton("采用候选脚本").disabled).toBe(true);
            expect(pageButton("生成脚本").disabled).toBe(true);
            await inputValue("商品名称", "Updated while away");
            await act(() => pageButton("采用候选脚本").click());
            expect((host.querySelector(".ad-shot textarea") as HTMLTextAreaElement).value).toBe("地铁通勤特写");
            expect(pageButton("确认脚本").disabled).toBe(true);
            expect(server.draft.shots).toHaveLength(0);
            await act(() => pageButton("保存草稿").click());
            await settle();
            expect(server.draft.shots).toHaveLength(2);
            expect(pageButton("采用候选脚本").disabled).toBe(true);
            expect(host.textContent).toContain("建议已进入当前草稿，尚需人工确认");
            expect(host.textContent).not.toContain("当前修订为 11；请重新生成");
            timeoutPost = true;
            missingPlanGets = 1;
            missingLatest = true;
            const initialGets = planGets;
            await act(() => pageButton("生成脚本").click());
            await settle();
            expect(planPosts).toBe(2);
            expect(dom.localStorage.getItem("ad-script-plan:ad:user:owner")).toBe(plan.operationId);
            expect(pageButton("生成脚本").disabled).toBe(true);
            await act(() => new Promise((resolve) => setTimeout(resolve, 1700)));
            expect(planGets - initialGets).toBeGreaterThanOrEqual(2);
            expect(host.textContent).toContain("来源修订 11");
            expect(host.textContent).not.toContain("提交结果未确认");
            server = {
                ...server,
                revision: 12,
                draft: { ...server.draft, productResourceIds: ["product-1", "product-2"] },
                resources: [...server.resources, { id: "product-2", title: "第二张商品图", mimeType: "image/png", available: true }],
            };
            await act(() => client.invalidateQueries({ queryKey: ["advertising", "owner", "ad"], exact: true }));
            await settle();
            expect(pageButton("生成脚本").disabled).toBe(true);
            expect(host.textContent).toContain("恰好绑定一张已就绪的商品图");
            oldConfirmation = true;
            server = {
                ...server,
                revision: 20,
                project: { ...server.project, name: "Newer server" },
                draft: { ...server.draft, brief: { ...server.draft.brief, productName: "Newer product" }, productResourceIds: [], shots: [] },
                resources: [],
                latestConfirmedRevisionId: "v2",
            };
            await act(() => pageButton("确认脚本").click());
            await settle();
            await act(() => pageButton("确认并冻结").click());
            await settle();
            expect((host.querySelector('[aria-label="商品名称"]') as any).value).toBe("Newer product");
            expect(dom.document.body.textContent).toContain("服务端修订已变化");
            await act(() => pageButton("继续编辑").click());
            const uploadInput = () => host.querySelector('input[type="file"]')!;
            const choose = async () => {
                const el = uploadInput();
                Object.defineProperty(el, "files", { configurable: true, value: [new dom.File(["png"], "product.png", { type: "image/png" })] });
                await act(() => el.dispatchEvent(new dom.Event("change", { bubbles: true })));
                await settle();
            };
            await choose();
            expect(host.textContent).toContain("仅本地，尚未可绑定");
            expect(server.draft.productResourceIds).toEqual([]);
            uploadResult = { storageKey: "resource:bound", mimeType: "image/png" };
            await choose();
            await act(() => pageButton("添加镜头").click());
            const reference = host.querySelector('input[type="checkbox"]')!;
            await act(() => reference.click());
            await act(() => pageButton("移除绑定").click());
            expect(host.textContent).toContain("请先在镜头中取消引用");
            expect(host.querySelector('input[type="checkbox"]')?.checked).toBe(true);
            await act(() => reference.click());
            await act(() => pageButton("移除绑定").click());
            expect(host.querySelector('a[href*="bound"]')).toBeNull();
            uploadResult = "wait";
            await choose();
            await act(() => {
                setActiveUserScope("second");
                useUserStore.setState({ user: { id: "second" } as any });
            });
            await settle();
            await act(() => finishUpload?.({ storageKey: "resource:late", mimeType: "image/png" }));
            await settle();
            expect(host.textContent).not.toContain("late");
            expect(client.getQueryData(["advertising", "second", "ad"])).toBeDefined();
            await act(() => router.navigate("/ad-projects/other"));
            await settle();
            expect(client.getQueryData(["advertising", "second", "other"])).toBeDefined();
        } finally {
            apiClient.defaults.adapter = original;
            client.clear();
            router.dispose();
            mock.restore();
        }
    } finally {
        await act(() => root.unmount());
        await dom.happyDOM.close();
    }
}, 45000);

test("confirmation rereview accepts same-content snapshots but rejects wrong content and ownership", async () => {
    if (!process.env.AD_CONFIRM_REPLAY_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path, "-t", "confirmation rereview"], { env: { ...process.env, AD_CONFIRM_REPLAY_CHECK: "1" }, encoding: "utf8", timeout: 30000 });
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
    const { createMemoryRouter, RouterProvider } = await import("react-router");
    const { AdvertisingEditor } = await import("../src/pages/ad-projects/detail");
    const { emptyAdvertisingDraft } = await import("../src/services/api/advertising-project");
    const { apiClient } = await import("../src/services/api/request");
    const { useUserStore } = await import("../src/stores/use-user-store");
    const { setActiveUserScope } = await import("../src/lib/user-scope");
    setActiveUserScope("replay-owner");
    useUserStore.setState({ user: { id: "replay-owner" } as any });
    const original = apiClient.defaults.adapter;
    const host = dom.document.createElement("div");
    dom.document.body.append(host);
    const root = createRoot(host as unknown as HTMLElement);
    const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 40)));
    const button = (text: string) => Array.from(dom.document.querySelectorAll("button")).find((b) => b.textContent?.replaceAll(" ", "") === text)!;
    try {
        for (const mode of ["lost-ack", "resave", "wrong-draft", "wrong-name", "wrong-project"]) {
            const id = "replay-" + mode;
            const draft = emptyAdvertisingDraft();
            draft.brief.description = "commute";
            draft.shots = [1, 2, 3].map((i) => ({ id: "shot-" + i, durationMs: 5000, visual: "product", voiceover: "", onScreenText: "", referenceResourceIds: [] }));
            let server: any = { project: { id, name: "Review" }, draft, revision: 1, resources: [] };
            let snapshot: any;
            const revisions: number[] = [];
            const productions: string[] = [];
            let planPosts = 0;
            let loseAck = mode === "lost-ack";
            apiClient.defaults.adapter = async (config) => {
                const url = config.url!;
                const body = config.data ? JSON.parse(String(config.data)) : {};
                let data: any;
                if (url.endsWith("/confirm")) {
                    revisions.push(body.expectedRevision);
                    expect(body.expectedRevision).toBe(server.revision);
                    if (!snapshot) {
                        // Same JSON, deliberately different key order from saved server content.
                        const reordered = Object.fromEntries(Object.entries(draft).reverse());
                        snapshot = { id: "v1", projectId: id, sourceRevision: server.revision, snapshot: { name: "Review", draft: reordered, resources: [] } };
                        server = { ...server, revision: server.revision + 1, latestConfirmedRevisionId: "v1" };
                    }
                    data = { currentRevision: server.revision, snapshot: structuredClone(snapshot) };
                    if (mode === "wrong-draft") data.snapshot.snapshot.draft.shots[0].visual = "not reviewed";
                    if (mode === "wrong-name") data.snapshot.snapshot.name = "not reviewed";
                    if (mode === "wrong-project") data.snapshot.projectId = "another-project";
                    if (loseAck) {
                        loseAck = false;
                        throw new Error("confirmation ACK lost");
                    }
                } else if (url.endsWith("/script-plans")) {
                    if (config.method === "post") planPosts++;
                    data = null;
                } else if (url.endsWith("/revisions")) data = { items: snapshot ? [snapshot] : [], total: snapshot ? 1 : 0 };
                else if (url.includes("/revisions/")) data = snapshot;
                else if (url.endsWith("/productions")) {
                    productions.push(body.confirmedRevisionId);
                    data = { production: { id: "p", revision: 1, confirmedRevisionId: "v1" }, settings: { ttsModelId: "", voiceId: "", selections: {}, musicResourceId: "", musicVolume: 0.15 }, confirmedSnapshot: snapshot };
                } else if (url.includes("/productions/")) data = { items: [], total: 0 };
                else if (url === "/model-catalog") data = { channels: [] };
                else if (url === "/plugins/catalog") data = { providers: [] };
                else if (config.method === "put") {
                    expect(body.expectedRevision).toBe(server.revision);
                    server = { ...server, project: { ...server.project, name: body.name }, draft: body.draft, revision: server.revision + 1 };
                    data = structuredClone(server);
                } else data = structuredClone(server);
                return { config, headers: {}, status: 200, statusText: "OK", data: { code: 0, data } };
            };
            const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
            const router = createMemoryRouter([{ path: "/ad", element: <AdvertisingEditor id={id} userId="replay-owner" /> }], { initialEntries: ["/ad"] });
            const review = async () => {
                await act(() => host.querySelector<HTMLButtonElement>('[aria-label="脚本确认"]')!.click());
                await act(() => button("确认脚本，下一步").click());
                await settle();
                await act(() => button("确认并冻结").click());
                await settle();
                await settle();
            };
            try {
                await act(() =>
                    root.render(
                        <QueryClientProvider client={client}>
                            <RouterProvider router={router} />
                        </QueryClientProvider>,
                    ),
                );
                await settle();
                await settle();
                await review();
                if (mode.startsWith("wrong-")) {
                    expect(dom.document.body.textContent).toContain("返回的确认并非本次核对版本");
                    expect(productions).toEqual([]);
                    continue;
                }
                if (mode === "lost-ack") {
                    expect(dom.document.body.textContent).toContain("confirmation ACK lost");
                    expect(productions).toEqual([]);
                    await act(() => button("确认并冻结").click());
                    await settle();
                    expect(dom.document.body.textContent).toContain("服务端修订已变化");
                    expect(revisions).toEqual([1]);
                    await act(() => button("继续编辑").click());
                    await settle();
                } else {
                    expect(productions).toEqual(["v1"]);
                    // An unchanged save is legal and advances revision without a new snapshot.
                    await apiClient.put("/ad-projects/" + id, { name: "Review", draft, expectedRevision: 2 });
                    await act(() => client.invalidateQueries({ queryKey: ["advertising", "replay-owner", id], exact: true }));
                    await settle();
                }
                await review();
                expect(revisions).toEqual(mode === "lost-ack" ? [1, 2] : [1, 3]);
                expect(dom.document.body.textContent).not.toContain("返回的确认并非本次核对版本");
                expect(host.querySelector('[aria-current="step"]')?.textContent).toContain("镜头制作");
                expect(productions).toEqual(["v1"]);
                expect(planPosts).toBe(0);
                // A real remote edit still requires another review, never blind confirmation.
                await act(() => host.querySelector<HTMLButtonElement>('[aria-label="脚本确认"]')!.click());
                await act(() => button("确认脚本，下一步").click());
                await settle();
                server = { ...server, revision: server.revision + 1, draft: { ...server.draft, brief: { ...server.draft.brief, description: "changed remotely" } } };
                await act(() => button("确认并冻结").click());
                await settle();
                expect(dom.document.body.textContent).toContain("服务端修订已变化");
                expect(revisions).toHaveLength(2);
                expect(productions).toEqual(["v1"]);
            } finally {
                await act(() => root.render(<div />));
                client.clear();
                router.dispose();
            }
        }
    } finally {
        await act(() => root.unmount());
        apiClient.defaults.adapter = original;
        await dom.happyDOM.close();
    }
}, 35000);
