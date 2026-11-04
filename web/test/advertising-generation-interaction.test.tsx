import { expect, test } from "bun:test";
import { Window } from "happy-dom";
import { spawnSync } from "node:child_process";

test("production requires explicit entry, fee approval and adoption", async () => {
    if (!process.env.AD_GENERATION_DOM_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path], { env: { ...process.env, AD_GENERATION_DOM_CHECK: "1" }, encoding: "utf8", timeout: 60000 });
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
        settings: { ttsModelId: "", voiceId: "", selections: {}, musicResourceId: "", musicVolume: 0.15 },
        confirmedSnapshot: { id: "v", sourceRevision: 1, snapshot: { name: "TEST", draft, resources: [] } },
    };
    let submissions: any[] = [];
    let executed = 0;
    let lost = true;
    let prepared = 0;
    let failSave = false;
    let expired = false;
    let finishQuote: (() => void) | undefined;
    let deferQuote = false;
    let failAssembly = false;
    const assemblyKeys: string[] = [];
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
        else if (url.endsWith("/execute")) {
            if (expired) {
                reason = "advertising_quote_expired";
            } else {
                executed++;
                submissions[0].task = { id: "task", status: "succeeded", resultJson: JSON.stringify({ video: { storageKey: "resource:video", durationMs: 5000 } }) };
                data = submissions[0];
                if (lost) {
                    lost = false;
                    throw new Error("response lost");
                }
            }
        } else if (url.endsWith("/submissions") && config.method === "post") {
            if (deferQuote)
                await new Promise<void>((resolve) => {
                    finishQuote = resolve;
                });
            prepared++;
            data = { id: "s", productionId: "p", shotId: "shot", kind: "video", publicRequest: { modelId: "m", prompt: "TEST" }, quote: { quoteHash: "hash", expiresAt: new Date(Date.now() + 60000).toISOString(), amountMicrocredits: 1000 } };
            submissions = [data];
        } else if (url.endsWith("/submissions")) data = { items: submissions, page: 1, pageSize: 20, total: submissions.length };
        else if (url.endsWith("/assemblies") && config.method === "post") {
            assemblyKeys.push(body.clientOperationId);
            if (failAssembly) throw new Error("镜头1视频实际4999ms，目标5000ms");
        } else if (url.endsWith("/assemblies")) data = { items: [], page: 1, pageSize: 20, total: 0 };
        else if (url.includes("/resources/")) data = { resource: { id: "video", status: "ready", mimeType: "video/mp4", durationMs: 5000 } };
        else if (config.method === "patch") {
            if (failSave) reason = "advertising_production_conflict";
            else {
                production = { ...production, production: { ...production.production, revision: production.production.revision + 1 }, settings: body.settings };
                data = production;
            }
        } else data = structuredClone(production);
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
        await act(() => button("进入此版本制作").click());
        await settle();
        await settle();
        expect(button("组装新时间线").disabled).toBe(true);
        const select = host.querySelector('[aria-label="镜头1视频模型"]')!;
        await act(() => {
            (select as any).value = "m";
            select.dispatchEvent(new dom.Event("change", { bubbles: true }));
        });
        await act(() => button("获取视频报价").click());
        await settle();
        expect(prepared).toBe(1);
        expect(executed).toBe(0);
        await act(() => button("查看并确认报价").click());
        await settle();
        await act(() => button("确认费用并生成").click());
        await settle();
        expect(executed).toBe(1);
        expect(host.textContent).toContain("response lost");
        await act(() => button("确认费用并生成").click());
        await settle();
        expect(prepared).toBe(1);
        expect(executed).toBe(2);
        await settle();
        expect(button("组装新时间线").disabled).toBe(true);
        await act(() => button("采用此视频").click());
        await settle();
        expect(production.settings.selections.shot.videoSubmissionId).toBe("s");
        expect(button("组装新时间线").disabled).toBe(false);
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
        failSave = true;
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
        await act(() => button("查看并确认报价").click());
        await settle();
        await act(() => button("确认费用并生成").click());
        await settle();
        expect(dom.document.body.textContent).toContain("报价已过期");
        expect(executed).toBe(2);
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
        expect(executed).toBe(2);
    } finally {
        await act(() => root.unmount());
        client.clear();
        apiClient.defaults.adapter = original;
        await dom.happyDOM.close();
    }
});
