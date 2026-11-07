import { expect, test, mock } from "bun:test";
import { Window } from "happy-dom";
import { spawnSync } from "node:child_process";

// Removing serialization, revision checks, or verified persistence breaks these checks.
test("autosave serializes edits, recovers scoped drafts and never invents durable saves", async () => {
    if (!process.env.AD_AUTOSAVE_DOM_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path], { env: { ...process.env, AD_AUTOSAVE_DOM_CHECK: "1" }, encoding: "utf8", timeout: 30000 });
        expect(child.stderr + child.stdout).not.toContain("(fail)");
        expect(child.status).toBe(0);
        return;
    }
    const dom = new Window({ url: "http://localhost" });
    for (const [key, value] of Object.entries({ window: dom, document: dom.document, navigator: dom.navigator, HTMLElement: dom.HTMLElement, IS_REACT_ACT_ENVIRONMENT: true }))
        Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
    const cache = new Map<string, string>();
    let storageMode = "ok";
    let delayRead = false;
    let releaseRead: (() => void) | undefined;
    mock.module("localforage", () => ({
        default: {
            config() {},
            async getItem(key: string) {
                const raw = cache.get(key) ?? null;
                if (delayRead) {
                    delayRead = false;
                    await new Promise<void>((resolve) => {
                        releaseRead = resolve;
                    });
                }
                return raw;
            },
            async setItem(key: string, value: string) {
                if (storageMode === "fail") throw new Error("quota");
                if (storageMode === "ok") cache.set(key, value);
            },
            async removeItem(key: string) {
                cache.delete(key);
            },
        },
    }));
    const { act } = await import("react");
    const { createRoot } = await import("react-dom/client");
    const { setActiveUserScope, scopedStorageKey, withUserScopedPersistenceSuppressed } = await import("../src/lib/user-scope");
    const modulePath = "../src/pages/ad-projects/use-advertising-autosave";
    const loaded = await import(modulePath).catch(() => ({}));
    expect(typeof loaded.useAdvertisingAutosave).toBe("function");
    const { useAdvertisingAutosave } = loaded;
    const host = dom.document.createElement("div");
    const root = createRoot(host as unknown as HTMLElement);
    let hook: any;
    let server = { revision: 1, value: "initial" };
    let scopeKey = "project:draft";
    let current = true;
    let offline = false;
    let initialAvailable = true;
    let conflict = false;
    let deferred = false;
    let release: (() => void) | undefined;
    const writes: { value: string; revision: number }[] = [];
    let inFlight = 0;
    let maximum = 0;
    const save = async (value: string, revision: number) => {
        writes.push({ value, revision });
        maximum = Math.max(maximum, ++inFlight);
        try {
            if (deferred)
                await new Promise<void>((resolve) => {
                    release = resolve;
                });
            if (offline) throw new Error("offline");
            if (conflict || revision !== server.revision) throw Object.assign(new Error("conflict"), { reason: "advertising_revision_conflict" });
            server = { revision: revision + 1, value };
            return server;
        } finally {
            inFlight--;
        }
    };
    function Probe() {
        hook = useAdvertisingAutosave({
            scopeKey,
            current: () => current,
            validate: (value: unknown) => typeof value === "string",
            initial: initialAvailable ? server : undefined,
            save,
            read: async () => {
                if (offline) throw new Error("offline");
                return server;
            },
        });
        return (
            <p>
                {hook.value}:{hook.status}
            </p>
        );
    }
    const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 10)));
    const mount = async () => {
        await act(() => root.render(<Probe key={scopeKey} />));
        await settle();
    };
    const flush = async () => {
        await act(async () => {
            await hook.flush().catch(() => {});
        });
        await settle();
    };
    const edit = async (value: string) => {
        await act(() => hook.setValue(value));
        await settle();
    };
    const unload = () => {
        const event = new dom.Event("beforeunload", { cancelable: true });
        dom.dispatchEvent(event);
        return event.defaultPrevented;
    };
    try {
        setActiveUserScope("owner");
        for (const liveEdit of [true, false]) {
            scopeKey = "restore-race:" + liveEdit;
            cache.set(scopedStorageKey("advertising-autosave:" + scopeKey), JSON.stringify({ base: { revision: 1, value: "old" }, value: "old unsynced" }));
            initialAvailable = false;
            delayRead = true;
            await mount();
            server = { revision: 8, value: "fresh server" };
            initialAvailable = true;
            await mount();
            if (liveEdit) await edit("new input");
            await act(() => releaseRead!());
            await settle();
            expect(hook.value).toBe(liveEdit ? "new input" : "old unsynced");
            expect(hook.revision).toBe(liveEdit ? 8 : 1);
            if (!liveEdit) expect(hook.conflict.remote).toEqual(server);
            server = { revision: 9, value: "later server" };
            await mount();
            expect(hook.value).toBe(liveEdit ? "new input" : "old unsynced");
            await act(() => root.render(<div />));
        }
        scopeKey = "project:draft";
        server = { revision: 1, value: "initial" };
        await mount();
        await edit("A");
        await act(() => new Promise((resolve) => setTimeout(resolve, 300)));
        expect(writes).toHaveLength(0);
        deferred = true;
        let first: Promise<unknown>;
        let second: Promise<unknown>;
        await act(() => {
            first = hook.flush();
            second = hook.flush();
        });
        await settle();
        await edit("B");
        expect(writes).toEqual([{ value: "A", revision: 1 }]);
        expect(unload()).toBe(true);
        deferred = false;
        await act(async () => {
            release!();
            await Promise.all([first!, second!]);
        });
        expect(hook.value).toBe("B");
        expect(server).toEqual({ revision: 3, value: "B" });
        expect(maximum).toBe(1);
        expect(hook.status).toBe("synced");
        expect(unload()).toBe(false);

        await edit("automatic");
        await act(() => new Promise((resolve) => setTimeout(resolve, 760)));
        expect(server.value).toBe("automatic");

        conflict = true;
        server = { revision: 9, value: "other device" };
        await edit("local conflict");
        await flush();
        expect(hook.status).toBe("conflict");
        expect(hook.value).toBe("local conflict");
        expect(hook.conflict.remote).toEqual(server);
        const count = writes.length;
        await edit("later local conflict");
        await act(() => new Promise((resolve) => setTimeout(resolve, 760)));
        expect(writes).toHaveLength(count);
        await act(() => hook.resolveConflict(hook.value));
        await act(() => new Promise((resolve) => setTimeout(resolve, 760)));
        expect(writes).toHaveLength(count);
        conflict = false;
        await flush();
        expect(server).toEqual({ revision: 10, value: "later local conflict" });

        offline = true;
        await edit("offline recovery");
        await flush();
        expect(hook.status).toBe("local-only");
        expect(unload()).toBe(false);
        await act(() => root.render(<div />));
        initialAvailable = false;
        await mount();
        expect(hook.value).toBe("offline recovery");
        expect(hook.status).not.toBe("synced");
        offline = false;
        server = { revision: 15, value: "newer server" };
        await flush();
        expect(hook.status).toBe("conflict");
        expect(server.value).toBe("newer server");
        await act(() => hook.resolveConflict(server.value));
        expect(hook.status).toBe("synced");

        for (const mode of ["fail", "noop", "suppressed"]) {
            storageMode = mode;
            offline = true;
            await act(() => {
                const update = () => hook.setValue(mode);
                if (mode === "suppressed") withUserScopedPersistenceSuppressed(update);
                else update();
            });
            await flush();
            expect(hook.status).toBe("failed");
            expect(unload()).toBe(true);
        }
        storageMode = "ok";
        offline = false;
        initialAvailable = true;
        scopeKey = "other-project:production:v2";
        server = { revision: 1, value: "other project" };
        await mount();
        expect(hook.value).toBe("other project");
        deferred = true;
        await edit("old user request");
        let pending: Promise<unknown>;
        await act(() => {
            pending = hook.flush().catch(() => {});
        });
        await settle();
        current = false;
        setActiveUserScope("second");
        scopeKey = "second:project:draft";
        server = { revision: 1, value: "second user" };
        current = true;
        await mount();
        deferred = false;
        await act(async () => {
            release!();
            await pending!;
        });
        expect(hook.value).toBe("second user");
        expect(hook.status).toBe("synced");
    } finally {
        await act(() => root.unmount());
        mock.restore();
        await dom.happyDOM.close();
    }
}, 35000);
