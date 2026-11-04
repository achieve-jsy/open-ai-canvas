import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { Button } from "antd";
import { PaginationBar } from "@/components/layout/workspace-page";
import { getActiveUserScope } from "@/lib/user-scope";
import { localForageStorageForScope } from "@/lib/localforage-storage";
import { useUserStore } from "@/stores/use-user-store";
import { getModelCatalog } from "@/services/api/logical-models";
import { fetchPluginProviderCatalog } from "@/services/api/plugin-catalog";
import { ApiError } from "@/services/api/request";
import { resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import { uploadMediaFile } from "@/services/file-storage";
import { canvasThemes } from "@/lib/canvas-theme";
import { useActiveTheme } from "@/stores/canvas/use-canvas-theme-store";
import type { Asset } from "@/stores/use-asset-store";
import * as api from "@/services/api/advertising-generation";
import { AdvertisingShotGeneration } from "./advertising-shot-generation";

const Preview = lazy(() => import("@/components/canvas/canvas-timeline-preview").then((m) => ({ default: m.CanvasTimelinePreview })));
const Picker = lazy(() => import("@/components/assets/asset-library-picker-modal").then((m) => ({ default: m.AssetLibraryPickerModal })));

export function AdvertisingProduction({ projectId, userId, confirmedRevisionId }: { projectId: string; userId: string; confirmedRevisionId?: string }) {
    const [entered, setEntered] = useState(false);
    if (!confirmedRevisionId)
        return (
            <section className="ad-production">
                <h2>广告制作</h2>
                <p>确认脚本后可进入制作；生成前必须逐镜头确认费用。</p>
            </section>
        );
    return (
        <section className="ad-production">
            <h2>广告制作</h2>
            <p>来源确认 {confirmedRevisionId}。与草稿及其他确认版本独立；切换版本不会迁移采用项。</p>
            {entered ? <ProductionEditor key={userId + ":" + projectId + ":" + confirmedRevisionId} projectId={projectId} userId={userId} revisionId={confirmedRevisionId} /> : <Button onClick={() => setEntered(true)}>进入此版本制作</Button>}
        </section>
    );
}

function ProductionEditor({ projectId, userId, revisionId }: { projectId: string; userId: string; revisionId: string }) {
    const client = useQueryClient();
    const theme = canvasThemes[useActiveTheme()];
    const [production, setProduction] = useState<api.AdvertisingProductionDetail>();
    const [settings, setSettings] = useState<api.AdvertisingProductionSettings>();
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const working = useRef(false);
    const [remote, setRemote] = useState<api.AdvertisingProductionDetail>();
    const [page, setPage] = useState(1);
    const [assemblyPage, setAssemblyPage] = useState(1);
    const [renderPage, setRenderPage] = useState(1);
    const [assembly, setAssembly] = useState<api.AdvertisingAssembly>();
    const [fit, setFit] = useState<"pad" | "crop">("pad");
    const [playhead, setPlayhead] = useState(0);
    const [playing, setPlaying] = useState(false);
    const [picker, setPicker] = useState(false);
    const alive = useRef(true);
    const scope = useRef(getActiveUserScope());
    const storage = useRef(localForageStorageForScope(scope.current));
    const current = () => alive.current && getActiveUserScope() === scope.current && useUserStore.getState().user?.id === userId;
    const key = ["advertising-production", userId, projectId, revisionId];
    const query = useQuery({ queryKey: key, queryFn: () => api.createAdvertisingProduction(projectId, revisionId), retry: false, refetchOnWindowFocus: false });
    useEffect(() => {
        alive.current = true;
        return () => {
            alive.current = false;
        };
    }, []);
    useEffect(() => {
        if (query.data && !production && current()) {
            setProduction(query.data);
            setSettings(query.data.settings);
        }
    }, [query.data]);
    const p = production?.production.id || "";
    const submissions = useQuery({ queryKey: [...key, "submissions", p, page], queryFn: ({ signal }) => api.listAdvertisingSubmissions(projectId, p, page, 20, signal), enabled: !!p, retry: false, refetchInterval: 4000 });
    const assemblies = useQuery({ queryKey: [...key, "assemblies", p, assemblyPage], queryFn: ({ signal }) => api.listAdvertisingAssemblies(projectId, p, assemblyPage, 20, signal), enabled: !!p, retry: false });
    const renders = useQuery({
        queryKey: [...key, "renders", assembly?.id, renderPage],
        queryFn: ({ signal }) => api.listAdvertisingRenders(projectId, p, assembly!.id, renderPage, 20, signal),
        enabled: !!p && !!assembly,
        retry: false,
        refetchInterval: 4000,
    });
    const catalog = useQuery({ queryKey: ["advertising-models", userId], queryFn: getModelCatalog, retry: false });
    const protocols = useQuery({ queryKey: ["advertising-protocols", userId], queryFn: () => fetchPluginProviderCatalog("admin.system-channel"), retry: false });
    const models = catalog.data?.channels?.flatMap((c) => c.models) || [];
    const invalidate = () => client.invalidateQueries({ queryKey: key });
    const operation = async <T,>(kind: string, input: unknown, send: (id: string) => Promise<T>): Promise<T> => {
        const storageKey = "advertising-operation:" + projectId + ":" + p + ":" + kind;
        const signature = JSON.stringify(input);
        const raw = await storage.current.getItem(storageKey);
        const prior = raw ? (JSON.parse(raw) as { signature: string; id: string }) : undefined;
        const id = prior?.signature === signature ? prior.id : crypto.randomUUID();
        await storage.current.setItem(storageKey, JSON.stringify({ signature, id }));
        if (!current()) throw new Error("账号或项目已切换，未发送操作");
        const result = await send(id);
        // Keep failed operation keys; delete only after a confirmed response.
        await storage.current.removeItem(storageKey);
        if (!current()) throw new Error("账号或项目已切换");
        return result;
    };
    const run = async (fn: () => Promise<void>) => {
        if (working.current || !current()) return;
        working.current = true;
        setBusy(true);
        setError("");
        try {
            await fn();
        } catch (e) {
            if (current()) setError(e instanceof Error ? e.message : "操作失败");
        } finally {
            working.current = false;
            if (current()) setBusy(false);
        }
    };
    const save = async (next: api.AdvertisingProductionSettings) => {
        if (!production || !current()) throw new Error("请重新读取制作");
        try {
            const result = await api.saveAdvertisingProduction(projectId, p, production.production.revision, next);
            if (!current()) return;
            setProduction(result);
            setSettings(result.settings);
            setRemote(undefined);
            client.setQueryData(key, result);
        } catch (e) {
            if (current() && e instanceof ApiError && e.reason === "advertising_production_conflict") {
                const latest = await api.getAdvertisingProduction(projectId, p);
                if (current()) setRemote(latest);
            }
            throw e;
        }
    };
    const quote = async (request: Omit<api.AdvertisingSubmissionRequest, "clientOperationId">) => {
        const result = await operation("quote:" + request.shotId + ":" + request.kind, request, (id) => api.prepareAdvertisingSubmission(projectId, p, { ...request, clientOperationId: id }));
        if (current()) {
            setPage(1);
            await invalidate();
        }
        return result;
    };
    const execute = async (s: api.AdvertisingSubmission) => {
        if (!current()) return;
        await api.executeAdvertisingSubmission(projectId, p, s.id, s.quote.quoteHash);
        if (current()) await invalidate();
    };
    const adopt = async (s: api.AdvertisingSubmission, start: number) => {
        if (!settings) return;
        const previous = settings.selections[s.shotId] || { videoSubmissionId: "", audioSubmissionId: "", videoStartMs: 0 };
        await save({ ...settings, selections: { ...settings.selections, [s.shotId]: { ...previous, ...(s.kind === "video" ? { videoSubmissionId: s.id, videoStartMs: start } : { audioSubmissionId: s.id }) } } });
    };
    const resourceReason = (asset: Asset) => {
        const data = asset.data as { storageKey?: string; pendingRemoteUpload?: boolean; mimeType?: string };
        return asset.kind !== "audio" || data.pendingRemoteUpload || !resourceIdFromStorageKey(data.storageKey) || !["audio/mpeg", "audio/mp4", "audio/wav", "audio/x-wav"].includes(data.mimeType || "") ? "请选择已上传的音频素材" : "";
    };
    if (query.error) return <p role="alert">{String(query.error)}</p>;
    if (!production || !settings) return <p role="status">读取此确认版本的制作状态…</p>;
    const shots = production.confirmedSnapshot.snapshot.draft.shots;
    const complete = shots.every((s) => settings.selections[s.id]?.videoSubmissionId && (!s.voiceover.trim() || settings.selections[s.id]?.audioSubmissionId));
    const dirty = JSON.stringify(settings) !== JSON.stringify(production.settings);
    const audioModel = models.find((m) => m.id === settings.ttsModelId);
    const voices = protocols.data?.find((p) => p.value === audioModel?.protocol)?.parameters?.find((p) => ["audioVoice", "voice"].includes(p.name))?.values || [];
    return (
        <>
            <p>
                制作修订 {production.production.revision} · 来源脚本修订 {production.confirmedSnapshot.sourceRevision}
            </p>
            <p>原视频固定静音。先试听候选再采用；组装会复核真实时长，缺镜头不会补黑片。</p>
            {(error || submissions.error || assemblies.error || renders.error || catalog.error || protocols.error) && <p role="alert">{error || String(submissions.error || assemblies.error || renders.error || catalog.error || protocols.error)}</p>}
            {remote && (
                <section role="alert">
                    <p>制作状态冲突，本地设置保留。服务端修订 {remote.production.revision}，请对照后选择。</p>
                    <details>
                        <summary>查看服务端设置</summary>
                        <pre className="ad-json">{JSON.stringify(remote.settings, null, 2)}</pre>
                    </details>
                    <Button
                        onClick={() => {
                            setProduction(remote);
                            setSettings(remote.settings);
                            setRemote(undefined);
                        }}
                    >
                        采用服务端设置
                    </Button>
                    <Button
                        onClick={() => {
                            setProduction(remote);
                            setRemote(undefined);
                        }}
                    >
                        保留本地设置继续编辑
                    </Button>
                </section>
            )}
            <fieldset disabled={busy || !!remote} className="ad-production-grid">
                <label>
                    默认旁白模型
                    <select value={settings.ttsModelId} onChange={(e) => setSettings({ ...settings, ttsModelId: e.target.value, voiceId: "" })}>
                        <option value="">不设置默认</option>
                        {models
                            .filter((m) => m.available && m.capability === "audio")
                            .map((m) => (
                                <option key={m.id} value={m.id}>
                                    {m.displayName}
                                </option>
                            ))}
                    </select>
                </label>
                <label>
                    默认音色
                    <select value={settings.voiceId} onChange={(e) => setSettings({ ...settings, voiceId: e.target.value })}>
                        <option value="">选择音色</option>
                        {voices.map((v) => (
                            <option key={v}>{v}</option>
                        ))}
                    </select>
                </label>
                <p>默认值仅用于新生成；不会修改已有候选或已采用旁白。</p>
                <label>
                    背景音乐
                    <select value={settings.musicResourceId} onChange={(e) => setSettings({ ...settings, musicResourceId: e.target.value })}>
                        <option value="">无背景音乐</option>
                        {production.confirmedSnapshot.snapshot.resources
                            .filter((r) => r.mimeType.startsWith("audio/"))
                            .map((r) => (
                                <option key={r.id} value={r.id}>
                                    {r.title}
                                </option>
                            ))}
                        {settings.musicResourceId && !production.confirmedSnapshot.snapshot.resources.some((r) => r.id === settings.musicResourceId) && <option value={settings.musicResourceId}>已绑定音频 {settings.musicResourceId}</option>}
                    </select>
                </label>
                <Button onClick={() => setPicker(true)}>从素材库选择音乐</Button>
                <label>
                    上传背景音乐
                    <input
                        type="file"
                        accept="audio/mpeg,audio/mp4,audio/wav,audio/x-wav"
                        onChange={(e) => {
                            const file = e.target.files?.[0];
                            e.target.value = "";
                            if (!file) return;
                            void run(async () => {
                                const result = await uploadMediaFile(file);
                                const id = resourceIdFromStorageKey(result.storageKey);
                                if (result.pendingRemoteUpload || !id) throw new Error("仅本地，尚未可绑定");
                                if (current()) setSettings((value) => (value ? { ...value, musicResourceId: id } : value));
                            });
                        }}
                    />
                </label>
                <label>
                    音乐音量（0–1）
                    <input type="number" min={0} max={1} step={0.05} value={settings.musicVolume} onChange={(e) => setSettings({ ...settings, musicVolume: e.target.valueAsNumber })} />
                </label>
                <Button onClick={() => void run(() => save(settings))}>保存制作设置</Button>
            </fieldset>
            {settings.musicResourceId && <audio controls src={resourceFileUrl(settings.musicResourceId)} aria-label="背景音乐试听" />}
            {shots.map((shot, index) => (
                <AdvertisingShotGeneration
                    key={shot.id + ":" + production.settings.ttsModelId + ":" + production.settings.voiceId}
                    shot={shot}
                    index={index}
                    models={models}
                    protocols={protocols.data || []}
                    submissions={submissions.data?.items || []}
                    selection={settings.selections[shot.id]}
                    defaults={production.settings}
                    disabled={busy || !!remote}
                    current={current}
                    quote={quote}
                    execute={execute}
                    adopt={adopt}
                />
            ))}
            <PaginationBar current={page} pageSize={20} total={submissions.data?.total || 0} pageSizeOptions={[20]} onChange={setPage} />
            <Button onClick={() => void submissions.refetch()}>刷新候选历史</Button>
            <fieldset disabled={busy || !!remote}>
                <label>
                    画幅适配
                    <select value={fit} onChange={(e) => setFit(e.target.value as "pad" | "crop")}>
                        <option value="pad">留边，保留完整商品</option>
                        <option value="crop">裁切铺满，可能裁掉边缘</option>
                    </select>
                </label>
                <Button
                    type="primary"
                    disabled={!complete || dirty}
                    onClick={() =>
                        void run(async () => {
                            const result = await operation("assembly", { revision: production.production.revision, fit }, (id) => api.createAdvertisingAssembly(projectId, p, production.production.revision, id, fit));
                            if (current()) {
                                setAssembly(result);
                                setAssemblyPage(1);
                                await invalidate();
                            }
                        })
                    }
                >
                    组装新时间线
                </Button>
                {dirty && <p>请先保存制作设置。</p>}
            </fieldset>
            <h3>组装历史</h3>
            {assemblies.data?.items.map((a) => (
                <Button
                    key={a.id}
                    onClick={() => {
                        setAssembly(a);
                        setPlayhead(0);
                        setPlaying(false);
                        setRenderPage(1);
                    }}
                >
                    组装 {a.id} · 制作修订 {a.sourceProductionRevision}
                </Button>
            ))}
            <PaginationBar current={assemblyPage} pageSize={20} total={assemblies.data?.total || 0} pageSizeOptions={[20]} onChange={setAssemblyPage} />
            {assembly && (
                <section>
                    <h3>当前组装 {assembly.id}</h3>
                    <p>
                        来源制作修订 {assembly.sourceProductionRevision} · 候选指纹 {assembly.selectionHash}
                    </p>
                    <Suspense fallback={<p>加载时间线预览…</p>}>
                        <Preview timeline={assembly.timeline} clips={assembly.timeline.clips} nodes={[]} playheadMs={playhead} playing={playing} theme={theme} onTogglePlay={() => setPlaying((v) => !v)} onPlayheadChange={setPlayhead} />
                    </Suspense>
                    <label>
                        预览位置
                        <input type="range" min={0} max={assembly.timeline.durationMs} value={playhead} onChange={(e) => setPlayhead(e.target.valueAsNumber)} />
                    </label>
                    <p>预览用于画面/文字/Logo检查；请分别试听音频，以导出MP4的混音为准。</p>
                    <div className="ad-actions">
                        <Button
                            disabled={busy}
                            onClick={() =>
                                void run(async () => {
                                    await operation("render:" + assembly.id, { assemblyId: assembly.id }, (id) => api.createAdvertisingRender(projectId, p, assembly.id, id));
                                    if (current()) {
                                        setRenderPage(1);
                                        await invalidate();
                                    }
                                })
                            }
                        >
                            导出原组装
                        </Button>
                        {assembly.canvasId ? <Link to={"/canvas/" + encodeURIComponent(assembly.canvasId)}>打开时间线继续编辑</Link> : <span>画布已删除；原组装仍保留。</span>}
                    </div>
                    <p>本地FFmpeg导出不调用模型、不扣模型积分，但占用服务器资源。手改画布的导出由任务中心管理，不覆盖原组装。</p>
                    {renders.data?.items.map((r) => (
                        <div key={r.id}>
                            <p>
                                导出任务 {r.task.id} · {r.task.status} · {r.task.stage} {r.task.error}
                            </p>
                            {r.available && r.task.status === "succeeded" && r.resourceId && (
                                <a href={resourceFileUrl(r.resourceId)} download>
                                    下载成片MP4
                                </a>
                            )}
                        </div>
                    ))}
                    <PaginationBar current={renderPage} pageSize={20} total={renders.data?.total || 0} pageSizeOptions={[20]} onChange={setRenderPage} />
                </section>
            )}
            {picker && (
                <Suspense fallback={<p>加载素材库…</p>}>
                    <Picker
                        open
                        title="选择背景音乐"
                        remoteLibrary
                        acceptRemoteItems
                        items={[]}
                        categoryLabels={{ all: "全部", material: "素材" }}
                        remoteKind="audio"
                        mediaKinds={["audio"]}
                        multiple={false}
                        remoteItemDisabledReason={resourceReason}
                        onClose={() => setPicker(false)}
                        onConfirm={(_, items) => {
                            const asset = items?.[0]?.asset;
                            if (!asset || resourceReason(asset)) {
                                setError("请选择已上传的音频素材");
                                return;
                            }
                            const id = resourceIdFromStorageKey((asset.data as { storageKey?: string }).storageKey);
                            if (id) setSettings({ ...settings, musicResourceId: id });
                            setPicker(false);
                        }}
                    />
                </Suspense>
            )}
        </>
    );
}
