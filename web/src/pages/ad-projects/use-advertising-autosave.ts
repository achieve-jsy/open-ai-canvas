import { useEffect, useMemo, useReducer, useRef, type SetStateAction } from "react";
import { localForageStorageForScope } from "@/lib/localforage-storage";
import { getActiveUserScope, isUserScopedPersistenceSuppressed } from "@/lib/user-scope";

export type AdvertisingSaved<T> = { revision: number; value: T };
export type AdvertisingSaveStatus = "loading" | "dirty" | "saving" | "synced" | "local-only" | "conflict" | "failed";
export const advertisingSaveLabel: Record<AdvertisingSaveStatus, string> = {
    loading: "正在恢复草稿…",
    dirty: "有未保存修改",
    saving: "正在保存…",
    synced: "已与服务端同步",
    "local-only": "仅保存在本机 · 尚未同步，换设备不可恢复",
    conflict: "服务端有新版本 · 本地修改已保留",
    failed: "未保存，离开可能丢失",
};
const equal = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

// Page-private: T is editable JSON only, never resources, media bytes or credentials.
export function useAdvertisingAutosave<T>(options: {
    scopeKey: string;
    current: () => boolean;
    validate: (value: unknown) => boolean;
    reconcile?: (value: T, remote: T) => T;
    initial?: AdvertisingSaved<T>;
    save: (value: T, expectedRevision: number) => Promise<AdvertisingSaved<T>>;
    read: () => Promise<AdvertisingSaved<T>>;
}) {
    const [, render] = useReducer((n: number) => n + 1, 0);
    const callbacks = useRef(options);
    callbacks.current = options;
    const scope = getActiveUserScope();
    const state = useMemo(
        () => ({
            scope,
            key: "advertising-autosave:" + options.scopeKey,
            active: true,
            storage: localForageStorageForScope(scope),
            base: undefined as AdvertisingSaved<T> | undefined,
            value: undefined as T | undefined,
            status: "loading" as AdvertisingSaveStatus,
            conflict: undefined as { remote?: AdvertisingSaved<T> } | undefined,
            ready: undefined as Promise<void> | undefined,
            restored: false,
            edited: false,
            flight: undefined as Promise<AdvertisingSaved<T>> | undefined,
            cache: Promise.resolve(),
            durable: "",
            retry: false,
            paused: false,
            timer: undefined as ReturnType<typeof setTimeout> | undefined,
        }),
        [scope, options.scopeKey],
    );
    const valid = () => state.active && callbacks.current.scopeKey === options.scopeKey && getActiveUserScope() === state.scope && callbacks.current.current();
    const notify = () => {
        if (valid()) render();
    };
    const dirty = () => !!state.base && !equal(state.value, state.base.value);
    const payload = () => JSON.stringify({ base: state.base, value: state.value, conflict: state.conflict, paused: state.paused });
    const persist = () => {
        const raw = payload();
        const allowed = !isUserScopedPersistenceSuppressed();
        state.cache = state.cache.then(async () => {
            try {
                if (!allowed || !valid() || typeof window === "undefined" || (typeof window.localStorage === "undefined" && typeof window.indexedDB === "undefined")) throw new Error("本机存储不可用");
                await state.storage.setItem(state.key, raw);
                if ((await state.storage.getItem(state.key)) !== raw) throw new Error("本机存储未写入");
                if (valid()) state.durable = raw;
            } catch {
                if (valid()) state.durable = "";
            }
            if (valid() && state.retry && !state.conflict && !state.flight) state.status = state.durable === payload() ? "local-only" : "failed";
            notify();
        });
        return state.cache;
    };
    const schedule = () => {
        clearTimeout(state.timer);
        if (state.restored && dirty() && !state.conflict && !state.paused)
            state.timer = setTimeout(() => {
                void flush().catch(() => {});
            }, 700);
    };
    const refreshConflict = async () => {
        if (!valid()) return;
        state.conflict = state.conflict || {};
        state.status = "conflict";
        notify();
        try {
            const remote = await callbacks.current.read();
            if (!valid()) return;
            state.conflict = { remote };
            state.status = "conflict";
            await persist();
        } finally {
            notify();
        }
    };
    // Explicit writes stay out of the editable/cache value until acknowledged.
    const flush = (explicit?: { update: (value: T) => T; accept: (value: T, remote: T) => T; write?: (value: T, expectedRevision: number) => Promise<AdvertisingSaved<T>>; recover?: boolean }): Promise<AdvertisingSaved<T>> => {
        clearTimeout(state.timer);
        if (state.flight) return explicit ? state.flight.then(() => flush(explicit)) : state.flight;
        const work = async () => {
            await state.ready;
            if (!valid() || !state.base) throw new Error("账号或项目已切换，或草稿尚未读取");
            if (state.conflict) throw new Error("请先比较并解决版本冲突");
            state.paused = false;
            try {
                if (state.retry) {
                    const remote = await callbacks.current.read();
                    if (!valid()) throw new Error("账号或项目已切换");
                    if (callbacks.current.reconcile) state.value = callbacks.current.reconcile(state.value as T, remote.value);
                    if (remote.revision !== state.base.revision) {
                        if (equal(remote.value, state.value)) state.base = remote;
                        else {
                            state.conflict = { remote };
                            throw new Error("服务端有新版本");
                        }
                    }
                    state.retry = false;
                }
                while (dirty() || explicit) {
                    if (!valid()) throw new Error("账号或项目已切换");
                    if (state.conflict) throw new Error("请先比较并解决版本冲突");
                    const action = dirty() && !explicit?.recover ? undefined : explicit;
                    const value = action ? action.update(state.value as T) : (state.value as T);
                    state.status = "saving";
                    notify();
                    let result: AdvertisingSaved<T>;
                    try {
                        result = await (action?.write || callbacks.current.save)(value, state.base.revision);
                    } catch (error) {
                        if (action && valid()) {
                            // Even a lost success must be read back before another settings write.
                            state.conflict = {};
                            await refreshConflict();
                            if (!valid() || !state.conflict?.remote) throw error;
                            const remote = state.conflict.remote;
                            state.value = action.accept(state.value as T, remote.value);
                            state.base = remote;
                            state.conflict = undefined;
                            explicit = undefined;
                            // A review endpoint must prove its own ACK; equal settings do not prove a decision.
                            if (action.write || !equal(action.accept(value, remote.value), value)) throw error;
                            result = remote;
                        } else throw error;
                    }
                    if (!valid()) throw new Error("账号或项目已切换");
                    state.base = result;
                    if (action) {
                        state.value = action.accept(state.value as T, result.value);
                        explicit = undefined;
                    } else if (equal(state.value, value)) state.value = result.value;
                    await persist();
                }
                state.status = "synced";
                return { revision: state.base.revision, value: state.value as T };
            } catch (error) {
                if (valid()) {
                    state.retry = true;
                    const reason = (error as { reason?: string }).reason;
                    if (reason === "advertising_revision_conflict" || reason === "advertising_production_conflict") {
                        state.conflict = {};
                        try {
                            await refreshConflict();
                        } catch {
                            /* Keep both the input and conflict gate if reading fails. */
                        }
                    }
                    await persist();
                    state.status = state.conflict ? "conflict" : state.durable === payload() ? "local-only" : "failed";
                }
                throw error;
            } finally {
                notify();
            }
        };
        state.flight = work().finally(() => {
            state.flight = undefined;
            notify();
        });
        return state.flight;
    };
    const setValue = (next: SetStateAction<T | undefined>) => {
        if (!valid() || !state.base) return;
        state.edited = true;
        state.value = typeof next === "function" ? (next as (value: T | undefined) => T | undefined)(state.value) : next;
        state.status = state.conflict ? "conflict" : state.flight ? "saving" : dirty() ? "dirty" : "synced";
        if (state.restored) void persist();
        schedule();
        notify();
    };
    const resolveConflict = (value: T) => {
        if (!valid() || !state.conflict?.remote) return;
        state.base = state.conflict.remote;
        state.value = callbacks.current.reconcile ? callbacks.current.reconcile(value, state.base.value) : value;
        state.conflict = undefined;
        state.retry = false;
        state.paused = true; // Resolving is not consent to overwrite: explicit flush/save is required.
        state.status = dirty() ? "dirty" : "synced";
        void persist();
        notify();
    };
    useEffect(() => {
        state.active = true;
        return () => {
            state.active = false;
            clearTimeout(state.timer);
        };
    }, [state]);
    useEffect(() => {
        const initial = options.initial;
        if (!valid()) return;
        if (state.ready) {
            if (!initial) return;
            if (!state.restored) {
                if (!state.edited && (!state.base || initial.revision > state.base.revision)) {
                    state.base = initial;
                    state.value = initial.value;
                    notify();
                }
                return;
            }
            if (!state.base) {
                state.base = initial;
                state.value = initial.value;
                if (state.status !== "failed") state.status = "synced";
                notify();
                return;
            }
            if (state.base && initial.revision > state.base.revision && !state.flight && !state.conflict) {
                if (dirty()) {
                    state.conflict = { remote: initial };
                    state.status = "conflict";
                    void persist();
                    notify();
                    return;
                }
                state.base = initial;
                state.value = initial.value;
                state.status = "synced";
                void persist();
                notify();
            }
            return;
        }
        state.ready = (async () => {
            state.base = initial;
            state.value = initial?.value;
            try {
                const raw = await state.storage.getItem(state.key);
                if (!valid()) return;
                const initial = callbacks.current.initial;
                if (!state.edited) {
                    state.base = initial;
                    state.value = initial?.value;
                }
                if (raw && !state.edited) {
                    const cached = JSON.parse(raw) as { base?: AdvertisingSaved<T>; value?: T; conflict?: { remote?: AdvertisingSaved<T> }; paused?: boolean };
                    const saved = (entry: AdvertisingSaved<T> | undefined): entry is AdvertisingSaved<T> => !!entry && Number.isSafeInteger(entry.revision) && entry.revision > 0 && callbacks.current.validate(entry.value);
                    if (
                        !cached ||
                        !saved(cached.base) ||
                        !callbacks.current.validate(cached.value) ||
                        (cached.paused !== undefined && typeof cached.paused !== "boolean") ||
                        (cached.conflict !== undefined && (!cached.conflict || typeof cached.conflict !== "object" || Array.isArray(cached.conflict) || (cached.conflict.remote !== undefined && !saved(cached.conflict.remote))))
                    )
                        throw new Error("无效草稿缓存");
                    if (!initial || !equal(cached.base.value, cached.value) || cached.conflict) {
                        state.base = cached.base;
                        state.value = cached.value;
                        state.paused = !!cached.paused;
                        state.conflict = cached.conflict;
                        state.retry = true;
                        if (initial && initial.revision !== cached.base.revision) state.conflict = { remote: initial };
                        state.durable = payload();
                    }
                }
                if (state.edited && initial && state.base && initial.revision > state.base.revision) state.conflict = { remote: initial };
                state.status = state.conflict ? "conflict" : state.retry ? "local-only" : dirty() ? "dirty" : state.base ? "synced" : "loading";
            } catch {
                if (valid()) state.status = "failed";
            }
            state.restored = true;
            if (state.edited && valid()) {
                void persist();
                schedule();
            }
            notify();
            // Recovered unsynced input requires a fresh server read; no blind background overwrite.
        })();
    }, [state, options.initial]);
    const unsafe = !!state.flight || (dirty() && state.durable !== payload());
    useEffect(() => {
        const unload = (event: BeforeUnloadEvent) => {
            if (valid() && (!!state.flight || (dirty() && state.durable !== payload()))) {
                event.preventDefault();
                event.returnValue = "";
            }
        };
        window.addEventListener("beforeunload", unload);
        return () => window.removeEventListener("beforeunload", unload);
    }, [state]);
    return { restored: state.restored, value: state.value, setValue, status: state.status, revision: state.base?.revision, dirty: dirty(), unsafe, flush, conflict: state.conflict, resolveConflict, refreshConflict };
}
