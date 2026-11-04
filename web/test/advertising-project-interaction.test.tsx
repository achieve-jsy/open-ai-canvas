import { expect, test, mock } from "bun:test";
import { Window } from "happy-dom";
import { spawnSync } from "node:child_process";

test("advertising form edits stable shots and explicit seconds without discarding fields", async () => {
    if (!process.env.ADVERTISING_DOM_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path], { env: { ...process.env, ADVERTISING_DOM_CHECK: "1" }, encoding: "utf8", timeout: 40000 });
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
        mock.module("../src/services/file-storage", () => ({
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
        let confirmations = 0;
        let oldConfirmation = false;
        const savedRevisions: number[] = [];
        const original = apiClient.defaults.adapter;
        apiClient.defaults.adapter = async (config) => {
            let data: any;
            const url = config.url!;
            const req = config.data ? JSON.parse(String(config.data)) : undefined;
            if (url.endsWith("/confirm") && oldConfirmation) {
                data = { currentRevision: 20, snapshot: { id: "v1", sourceRevision: 8, snapshot: { name: "Old", draft: emptyAdvertisingDraft(), resources: [] } } };
            } else if (url.endsWith("/confirm")) {
                confirmations++;
                server = { ...server, revision: server.revision + 1, latestConfirmedRevisionId: "v1" };
                data = { currentRevision: server.revision, snapshot: { id: "v1", sourceRevision: server.revision - 1, snapshot: { name: server.project.name, draft: server.draft, resources: [] } } };
            } else if (config.method === "put") {
                if (failSave) return { config, headers: {}, status: 200, statusText: "OK", data: { code: 409, msg: "保存失败", reason: failSave } };
                savedRevisions.push(req.expectedRevision);
                server = { ...server, project: { ...server.project, name: req.name }, draft: req.draft, revision: server.revision + 1 };
                data = structuredClone(server);
            } else if (url.endsWith("/revisions")) data = { items: [], total: 0, page: 1, pageSize: 20 };
            else if (url.includes("/revisions/")) data = { id: "v1", sourceRevision: server.revision - 1, snapshot: { name: server.project.name, draft: server.draft, resources: [] } };
            else data = { ...structuredClone(server), project: { ...server.project, id: url.endsWith("/other") ? "other" : "ad" } };
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
            await act(() =>
                root.render(
                    <QueryClientProvider client={client}>
                        <RouterProvider router={router} />
                    </QueryClientProvider>,
                ),
            );
            await settle();
            await act(() => pageButton("保存草稿").click());
            await settle();
            expect(savedRevisions).toEqual([1]);
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
            // Refetch must not replace active edits; remount must recover saved backend state.
            await inputValue("商品名称", "未保存");
            await act(async () => {
                await client.invalidateQueries({ queryKey: ["advertising", "owner", "ad"] });
            });
            await settle();
            expect((host.querySelector('[aria-label="商品名称"]') as any).value).toBe("未保存");
            server = { ...server, revision: 10, draft: { ...server.draft, brief: { ...server.draft.brief, productName: "Updated while away" } } };
            await act(() => root.render(<div />));
            await act(() =>
                root.render(
                    <QueryClientProvider client={client}>
                        <RouterProvider router={router} />
                    </QueryClientProvider>,
                ),
            );
            await settle();
            expect((host.querySelector('[aria-label="商品名称"]') as any).value).toBe("Updated while away");
            oldConfirmation = true;
            server = { ...server, revision: 20, project: { ...server.project, name: "Newer server" }, draft: { ...server.draft, brief: { ...server.draft.brief, productName: "Newer product" } }, latestConfirmedRevisionId: "v2" };
            await act(() => pageButton("确认脚本").click());
            await settle();
            await act(() => pageButton("确认并冻结").click());
            await settle();
            expect((host.querySelector('[aria-label="商品名称"]') as any).value).toBe("Newer product");
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
