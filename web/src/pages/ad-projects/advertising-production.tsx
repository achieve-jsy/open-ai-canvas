import { advertisingCandidateLabel } from "./advertising-candidate-label";
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { Button } from "antd";
import { PaginationBar } from "@/components/layout/workspace-page";
import { getActiveUserScope } from "@/lib/user-scope";
import { localForageStorageForScope } from "@/lib/localforage-storage";
import { useUserStore } from "@/stores/use-user-store";
import { getModelCatalog } from "@/services/api/logical-models";
import { fetchPluginProviderCatalog } from "@/services/api/plugin-catalog";
import { useAdvertisingAutosave, advertisingSaveLabel } from "./use-advertising-autosave";
import { resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import { uploadMediaFile } from "@/services/file-storage";
import { canvasThemes } from "@/lib/canvas-theme";
import { useActiveTheme } from "@/stores/canvas/use-canvas-theme-store";
import type { Asset } from "@/stores/use-asset-store";
import * as api from "@/services/api/advertising-generation";
import { createPortal } from "react-dom";
import type { AdvertisingStage } from "./advertising-workbench";
import { AdvertisingShotGeneration } from "./advertising-shot-generation";
import { AdvertisingReviewPanel } from "./advertising-review-panel";
import type { CandidateDecision } from "./advertising-candidate-studio";
import { AdvertisingCutReview, type ReviewIntent } from "./advertising-cut-review";
import { ApiError } from "@/services/api/request";

const Preview = lazy(() => import("@/components/canvas/canvas-timeline-preview").then((m) => ({ default: m.CanvasTimelinePreview })));
const Picker = lazy(() => import("@/components/assets/asset-library-picker-modal").then((m) => ({ default: m.AssetLibraryPickerModal })));

export function validCachedSettings(value: unknown): value is api.AdvertisingProductionSettings {
    const v = value as api.AdvertisingProductionSettings | null;
    return (
        !!v &&
        [v.ttsModelId, v.voiceId, v.musicResourceId].every((item) => typeof item === "string") &&
        ["pad", "crop"].includes(v.fit) &&
        Number.isFinite(v.musicVolume) &&
        v.musicVolume >= 0 &&
        v.musicVolume <= 1 &&
        !!v.selections &&
        typeof v.selections === "object" &&
        !Array.isArray(v.selections) &&
        Object.values(v.selections).every(
            (selection) =>
                !!selection &&
                typeof selection.videoSubmissionId === "string" &&
                typeof selection.audioSubmissionId === "string" &&
                Number.isSafeInteger(selection.videoStartMs) &&
                selection.videoStartMs >= 0 &&
                (!!selection.videoSubmissionId || selection.videoStartMs === 0),
        )
    );
}

type ProductionPresentation = {
    stage?: AdvertisingStage;
    autoEnter?: boolean;
    actionTarget?: HTMLDivElement | null;
    onStageChange?: (stage: AdvertisingStage) => void;
    onStateChange?: (state: { unsafe: boolean; status: string; selected: number; total: number; assembly: boolean; downloadable: boolean }) => void;
};
export function AdvertisingProduction({ projectId, userId, confirmedRevisionId, autoEnter = false, ...presentation }: { projectId: string; userId: string; confirmedRevisionId?: string } & ProductionPresentation) {
    const [entered, setEntered] = useState(autoEnter);
    useEffect(() => {
        if (autoEnter) setEntered(true);
    }, [autoEnter]);
    if (!confirmedRevisionId)
        return (
            <section className="ad-production">
                <h2>广告制作</h2>
                <p>确认脚本后可进入制作；生成前必须逐镜头确认费用。</p>
                {presentation.actionTarget &&
                    createPortal(
                        <Button type="primary" onClick={() => presentation.onStageChange?.("script")}>
                            返回脚本确认
                        </Button>,
                        presentation.actionTarget,
                    )}
            </section>
        );
    return (
        <section className="ad-production">
            <h2>广告制作</h2>
            <p>来源确认 {confirmedRevisionId}。与草稿及其他确认版本独立；切换版本不会迁移采用项。</p>
            {entered ? (
                <ProductionEditor key={userId + ":" + projectId + ":" + confirmedRevisionId} projectId={projectId} userId={userId} revisionId={confirmedRevisionId} {...presentation} />
            ) : (
                <>
                    <Button onClick={() => setEntered(true)}>进入此版本制作</Button>
                    {presentation.actionTarget &&
                        createPortal(
                            <Button type="primary" onClick={() => setEntered(true)}>
                                进入此版本制作
                            </Button>,
                            presentation.actionTarget,
                        )}
                </>
            )}
        </section>
    );
}

function ProductionEditor({ projectId, userId, revisionId, stage, actionTarget, onStateChange, onStageChange }: { projectId: string; userId: string; revisionId: string } & ProductionPresentation) {
    const client = useQueryClient();
    const theme = canvasThemes[useActiveTheme()];
    const [production, setProduction] = useState<api.AdvertisingProductionDetail>();
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const working = useRef(false);
    const [shotBusy, setShotBusy] = useState<Record<string, boolean>>({});
    const onShotBusy = useCallback((id: string, busy: boolean) => setShotBusy((prior) => ({ ...prior, [id]: busy })), []);
    const [page, setPage] = useState(1);
    const [rework, setRework] = useState<{ issue: api.AdvertisingIssue; submission: api.AdvertisingSubmission }>();
    const [pendingReview, setPendingReview] = useState<api.AdvertisingReviewRequest>();
    const [reviewMissing, setReviewMissing] = useState(false);
    const [pendingCutIssue, setPendingCutIssue] = useState<api.AdvertisingIssueRequest>();
    const [cutIssueMissing, setCutIssueMissing] = useState(false);
    const [assemblyPage, setAssemblyPage] = useState(1);
    const [renderPage, setRenderPage] = useState(1);
    const [assembly, setAssembly] = useState<api.AdvertisingAssembly>();
    const [selectedOutput, setSelectedOutput] = useState<api.AdvertisingRender>();
    const [fitReady, setFitReady] = useState(false);
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
        if (query.data && current() && (!production || query.data.production.revision >= production.production.revision)) {
            setProduction(query.data);
        }
    }, [query.data]);
    const p = production?.production.id || "";
    const editable = (data: api.AdvertisingProductionDetail) => ({ revision: data.production.revision, value: data.settings });
    const reviewKey = "advertising-review:" + projectId + ":" + p;
    const rememberProduction = (result: api.AdvertisingProductionDetail) => {
        if (!current()) return;
        setProduction((prior) => (!prior || result.production.revision >= prior.production.revision ? result : prior));
        client.setQueryData<api.AdvertisingProductionDetail>(key, (prior) => (!prior || result.production.revision >= prior.production.revision ? result : prior));
    };
    const readPendingReview = async () => {
        const raw = await storage.current.getItem(reviewKey);
        if (!raw) return;
        const saved = JSON.parse(raw);
        const r = saved?.request;
        if (
            saved?.projectId !== projectId ||
            saved?.productionId !== p ||
            !r ||
            typeof r.clientOperationId !== "string" ||
            !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(r.clientOperationId) ||
            !["candidate", "assembly", "delivery"].includes(r.checkpoint) ||
            (r.checkpoint === "candidate"
                ? typeof r.submissionId !== "string" || !r.submissionId || r.assemblyId !== undefined || r.renderAttemptId !== undefined || r.resourceId !== undefined || r.acceptedReviewId !== undefined
                : [r.assemblyId, r.renderAttemptId, r.resourceId].some((v) => typeof v !== "string" || !v) ||
                  r.submissionId !== undefined ||
                  r.clearSelected !== undefined ||
                  (r.checkpoint === "delivery" ? typeof r.acceptedReviewId !== "string" || !r.acceptedReviewId || r.decision !== "approve" : r.acceptedReviewId !== undefined)) ||
            !Number.isSafeInteger(r.expectedProductionRevision) ||
            r.expectedProductionRevision < 1 ||
            !["approve", "reject"].includes(r.decision) ||
            typeof r.reason !== "string" ||
            !r.reason.trim() ||
            r.reason.length > 2000 ||
            (r.clearSelected !== undefined && (typeof r.clearSelected !== "boolean" || (r.clearSelected && r.decision !== "reject")))
        )
            throw new Error("原验收记录不完整，需人工核对");
        const request: api.AdvertisingReviewRequest = {
            clientOperationId: r.clientOperationId,
            expectedProductionRevision: r.expectedProductionRevision,
            decision: r.decision,
            reason: r.reason,
            ...(r.checkpoint === "candidate"
                ? { checkpoint: "candidate", submissionId: r.submissionId, ...(r.clearSelected !== undefined ? { clearSelected: r.clearSelected } : {}) }
                : { checkpoint: r.checkpoint, assemblyId: r.assemblyId, renderAttemptId: r.renderAttemptId, resourceId: r.resourceId, ...(r.checkpoint === "delivery" ? { acceptedReviewId: r.acceptedReviewId } : {}) }),
        };
        if (current()) setPendingReview(request);
        return request;
    };
    const readProduction = async (clearAcknowledged = false) => {
        if (!p) throw new Error("制作尚未读取");
        const pending = await readPendingReview();
        let acknowledged = false;
        if (pending) {
            try {
                await api.getAdvertisingReviewForOperation(projectId, p, pending.clientOperationId);
                acknowledged = true;
            } catch (e) {
                if (!(e instanceof ApiError && (e.status === 404 || e.reason === "not_found"))) throw e;
                if (current()) setReviewMissing(true);
            }
        }
        const result = await api.getAdvertisingProduction(projectId, p);
        if (!current()) throw new Error("账号或制作已切换");
        if (acknowledged && clearAcknowledged) {
            await storage.current.removeItem(reviewKey);
            setPendingReview(undefined);
            setReviewMissing(false);
        }
        const cached = client.getQueryData<api.AdvertisingProductionDetail>(key);
        const latest = cached && cached.production.revision > result.production.revision ? cached : result;
        rememberProduction(latest);
        return editable(latest);
    };
    useEffect(() => {
        if (p)
            void readPendingReview().catch((e) => {
                if (current()) setError(String(e));
            });
    }, [p]);
    const autosave = useAdvertisingAutosave({
        scopeKey: userId + ":" + projectId + ":production:" + revisionId,
        current,
        validate: validCachedSettings,
        reconcile: (value, remote) => ({ ...value, selections: remote.selections }),
        initial: query.data && editable(query.data),
        save: async (value, expectedRevision) => {
            if (await readPendingReview()) throw new Error("原验收结果待核对；请查询原验收或明确重试原提交，普通保存不会重放批准");
            const result = await api.saveAdvertisingProduction(projectId, p, expectedRevision, value);
            rememberProduction(result);
            return editable(result);
        },
        read: () => readProduction(),
    });
    const { value: settings, setValue: setSettings } = autosave;
    const fit = settings?.fit || "pad";
    const remote = autosave.conflict?.remote;
    useEffect(() => {
        const unload = (event: BeforeUnloadEvent) => {
            if (busy && current()) {
                event.preventDefault();
                event.returnValue = "";
            }
        };
        window.addEventListener("beforeunload", unload);
        return () => window.removeEventListener("beforeunload", unload);
    }, [busy]);
    const roughCutStorageKey = "advertising-roughcut:" + projectId + ":" + p;
    useEffect(() => {
        if (!p) return;
        let active = true;
        void (async () => {
            try {
                const raw = await storage.current.getItem(roughCutStorageKey);
                if (!active || !current()) return;
                if (raw) {
                    JSON.parse(raw);
                }
                setFitReady(true);
            } catch {
                if (active && current()) setError("读取粗剪状态失败，请重新进入制作");
            }
        })();
        return () => {
            active = false;
        };
    }, [p]);
    const submissions = useQuery({ queryKey: [...key, "submissions", p, page], queryFn: ({ signal }) => api.listAdvertisingSubmissions(projectId, p, page, 20, signal), enabled: !!p, retry: false, refetchInterval: 4000 });
    const assemblies = useQuery({ queryKey: [...key, "assemblies", p, assemblyPage], queryFn: ({ signal }) => api.listAdvertisingAssemblies(projectId, p, assemblyPage, 20, signal), enabled: !!p, retry: false });
    const renders = useQuery({
        queryKey: [...key, "renders", assembly?.id, renderPage],
        queryFn: ({ signal }) => api.listAdvertisingRenders(projectId, p, assembly!.id, renderPage, 20, signal),
        enabled: !!p && !!assembly,
        retry: false,
        refetchInterval: 4000,
    });
    const assemblyState = useQuery({
        queryKey: [...key, "assembly-current", assembly?.id, production?.production.revision],
        queryFn: ({ signal }) => api.getAdvertisingAssembly(projectId, p, assembly!.id, signal),
        enabled: !!p && !!assembly,
        retry: false,
        refetchInterval: 4000,
    });
    const shownAssembly = assemblyState.data?.id === assembly?.id ? assemblyState.data : assembly;
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
    const cutIssueKey = "advertising-cut-issue:" + projectId + ":" + p;
    const readCutIssue = async () => {
        const raw = await storage.current.getItem(cutIssueKey);
        if (!raw) return;
        const saved = JSON.parse(raw),
            r = saved?.request;
        if (
            saved?.projectId !== projectId ||
            saved?.productionId !== p ||
            !r ||
            typeof r.clientOperationId !== "string" ||
            !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(r.clientOperationId) ||
            [r.assemblyId, r.renderAttemptId, r.note].some((v) => typeof v !== "string" || !v) ||
            r.note.length > 2000 ||
            r.category !== "other" ||
            !Number.isSafeInteger(r.atMs) ||
            r.atMs < 0
        )
            throw new Error("原成片问题记录不完整，需人工核对");
        const request: api.AdvertisingIssueRequest = { clientOperationId: r.clientOperationId, assemblyId: r.assemblyId, renderAttemptId: r.renderAttemptId, atMs: r.atMs, note: r.note, category: "other" };
        if (current()) setPendingCutIssue(request);
        return request;
    };
    useEffect(() => {
        if (p)
            void readCutIssue().catch((e) => {
                if (current()) setError(String(e));
            });
    }, [p]);
    const cutIssue = async (intent?: api.AdvertisingIssueRequest, retry = false) => {
        if (working.current || !current()) throw new Error("操作进行中或制作已切换");
        working.current = true;
        setBusy(true);
        try {
            const prior = await readCutIssue();
            if (prior) {
                try {
                    await api.getAdvertisingIssueForOperation(projectId, p, prior.clientOperationId);
                    if (!current()) throw new Error("制作已切换");
                    await storage.current.removeItem(cutIssueKey);
                    setPendingCutIssue(undefined);
                    setCutIssueMissing(false);
                    if (intent) throw new Error("原成片问题已恢复；当前新编辑尚未提交");
                    return;
                } catch (e) {
                    if (!(e instanceof ApiError && (e.status === 404 || e.reason === "not_found"))) throw e;
                    setCutIssueMissing(true);
                    if (!retry) throw new Error("原成片问题未入库；请明确重试原提交，不能替换为新问题");
                }
            }
            const request = prior || intent;
            if (!request) return;
            const saved = JSON.stringify({ projectId, productionId: p, request });
            await storage.current.setItem(cutIssueKey, saved);
            if (!current() || (await storage.current.getItem(cutIssueKey)) !== saved) throw new Error("问题操作键无法可靠保存，未发送");
            setPendingCutIssue(request);
            await api.createAdvertisingIssue(projectId, p, request);
            if (!current()) throw new Error("制作已切换");
            await storage.current.removeItem(cutIssueKey);
            setPendingCutIssue(undefined);
            setCutIssueMissing(false);
        } finally {
            working.current = false;
            if (current()) setBusy(false);
        }
    };
    const submitReview = async (intent: ReviewIntent) => {
        await autosave.flush({
            recover: !!pendingReview,
            update: (value) => value,
            accept: (value, remote) => ({ ...value, selections: remote.selections }),
            write: async (_, expectedProductionRevision) => {
                if (!current()) throw new Error("账号或制作已切换");
                const prior = await readPendingReview();
                const same =
                    prior &&
                    ["checkpoint", "submissionId", "assemblyId", "renderAttemptId", "resourceId", "acceptedReviewId", "decision", "reason"].every((key) => prior[key as keyof typeof prior] === intent[key as keyof typeof intent]) &&
                    !!prior.clearSelected === !!intent.clearSelected;
                if (prior) {
                    try {
                        await api.getAdvertisingReviewForOperation(projectId, p, prior.clientOperationId);
                        const recovered = await readProduction(true);
                        if (!same) throw new Error("已恢复原验收；当前不同决定尚未提交，请重新核对并明确批准");
                        return recovered;
                    } catch (e) {
                        if (!(e instanceof ApiError && (e.status === 404 || e.reason === "not_found"))) throw e;
                        setReviewMissing(true);
                        if (!same) throw new Error("原验收尚未入库；请先重试原提交，不能替换为当前决定");
                    }
                }
                const request: api.AdvertisingReviewRequest = prior || { clientOperationId: crypto.randomUUID(), expectedProductionRevision, ...intent };
                const saved = JSON.stringify({ projectId, productionId: p, request });
                await storage.current.setItem(reviewKey, saved);
                if ((await storage.current.getItem(reviewKey)) !== saved || !current()) throw new Error("无法可靠保存验收操作键，未发送");
                setPendingReview(request);
                try {
                    await api.createAdvertisingReview(projectId, p, request);
                } catch (error) {
                    if (error instanceof ApiError && !!error.reason && !error.retryable) {
                        await storage.current.removeItem(reviewKey);
                        setPendingReview(undefined);
                        setReviewMissing(false);
                        throw error;
                    }
                    const recovered = await readProduction(true);
                    if (await readPendingReview()) throw error;
                    return recovered;
                }
                const result = await readProduction(true);
                await client.invalidateQueries({ queryKey: ["advertising-candidates", scope.current, projectId, p] });
                return result;
            },
        });
    };
    const review = (s: Pick<api.AdvertisingSubmission, "id">, decision: CandidateDecision) => submitReview({ checkpoint: "candidate", submissionId: s.id, ...decision });
    const createAssembly = async () => {
        const synced = await autosave.flush();
        if (!current()) throw new Error("账号或项目已切换");
        const result = await operation("assembly", { revision: synced.revision, fit }, (id) => api.createAdvertisingAssembly(projectId, p, synced.revision, id, fit));
        if (current()) {
            setAssembly(result);
            setAssemblyPage(1);
            await invalidate();
        }
        return result;
    };
    const createRender = async (target: api.AdvertisingAssembly) => {
        await autosave.flush();
        if (!current()) return;
        // Explicit command only: reuse an existing output or active task before starting work.
        let page = 1;
        for (;;) {
            const history = await api.listAdvertisingRenders(projectId, p, target.id, page, 100);
            const existing = history.items.find((r) => r.available && r.resourceId && r.task.status === "succeeded");
            if (!current()) throw new Error("账号或制作已切换");
            if (existing) {
                setSelectedOutput(existing);
                return;
            }
            if (history.items.some((r) => !["succeeded", "failed", "cancelled"].includes(r.task.status) || r.task.canRecoverMedia)) {
                await invalidate();
                return;
            }
            if (page * 100 >= history.total) break;
            page++;
        }
        const result = await operation("render:" + target.id, { assemblyId: target.id }, (id) => api.createAdvertisingRender(projectId, p, target.id, id));
        if (current() && result.available && result.resourceId) setSelectedOutput(result);
        if (current()) {
            setRenderPage(1);
            await invalidate();
        }
    };
    const selectedCount =
        production?.confirmedSnapshot.snapshot.draft.shots.filter((shot) => production.settings.selections[shot.id]?.videoSubmissionId && (!shot.voiceover.trim() || production.settings.selections[shot.id]?.audioSubmissionId)).length || 0;
    const shotCount = production?.confirmedSnapshot.snapshot.draft.shots.length || 0;
    const currentRender = renders.data?.items.find((r) => r.id === selectedOutput?.id);
    const download = selectedOutput && selectedOutput.assemblyId === assembly?.id ? { ...selectedOutput, available: !!currentRender?.available && currentRender.resourceId === selectedOutput?.resourceId } : undefined;
    const downloadable = !!download?.available;
    const unsafe = autosave.unsafe || busy || !!pendingReview || !!pendingCutIssue || Object.values(shotBusy).some(Boolean);
    useEffect(() => {
        onStateChange?.({ unsafe, status: advertisingSaveLabel[autosave.status], selected: selectedCount, total: shotCount, assembly: !!assembly, downloadable });
    }, [unsafe, autosave.status, selectedCount, shotCount, !!assembly, downloadable, onStateChange]);
    useEffect(() => () => onStateChange?.({ unsafe: false, status: "尚未进入制作", selected: 0, total: 0, assembly: false, downloadable: false }), [onStateChange]);
    useEffect(() => {
        if (!assembly && assemblies.data?.items[0]) setAssembly(assemblies.data.items[0]);
    }, [assemblies.data, assembly]);
    const resourceReason = (asset: Asset) => {
        const data = asset.data as { storageKey?: string; pendingRemoteUpload?: boolean; mimeType?: string };
        return asset.kind !== "audio" || data.pendingRemoteUpload || !resourceIdFromStorageKey(data.storageKey) || !["audio/mpeg", "audio/mp4", "audio/wav", "audio/x-wav"].includes(data.mimeType || "") ? "请选择已上传的音频素材" : "";
    };
    if (query.error)
        return (
            <section>
                <p role="alert">{String(query.error)}</p>
                <Button onClick={() => void query.refetch()}>重新连接服务端制作</Button>
                {actionTarget &&
                    createPortal(
                        <Button type="primary" onClick={() => void query.refetch()}>
                            重试创建或恢复制作
                        </Button>,
                        actionTarget,
                    )}
                {settings && (
                    <>
                        <p role="status">{advertisingSaveLabel[autosave.status]}。尚未读取确认版本，不能生成或组装。</p>
                        <details>
                            <summary>查看本机制作设置</summary>
                            <pre className="ad-json">{JSON.stringify(settings, null, 2)}</pre>
                        </details>
                    </>
                )}
            </section>
        );
    if (!autosave.restored || !production || !settings || autosave.status === "loading") return <p role="status">读取此确认版本的制作状态…</p>;
    const shots = production.confirmedSnapshot.snapshot.draft.shots;
    const complete = shots.length > 0 && shots.every((s) => settings.selections[s.id]?.videoSubmissionId && (!s.voiceover.trim() || settings.selections[s.id]?.audioSubmissionId));
    const dirty = autosave.dirty;
    const audioModel = models.find((m) => m.id === settings.ttsModelId);
    const voices = protocols.data?.find((p) => p.value === audioModel?.protocol)?.parameters?.find((p) => ["audioVoice", "voice"].includes(p.name))?.values || [];
    return (
        <>
            <p>
                制作修订 {autosave.revision} · 来源脚本修订 {production.confirmedSnapshot.sourceRevision} · {advertisingSaveLabel[autosave.status]}
            </p>
            <p>原视频固定静音。先试听候选再采用；组装会复核真实时长，缺镜头不会补黑片。</p>
            {(error || submissions.error || assemblies.error || renders.error || catalog.error || protocols.error) && <p role="alert">{error || String(submissions.error || assemblies.error || renders.error || catalog.error || protocols.error)}</p>}
            {pendingReview && (
                <section>
                    <p>
                        原验收待核对：{pendingReview.checkpoint === "candidate" ? "候选 " + advertisingCandidateLabel(pendingReview.submissionId!) : "成片 " + pendingReview.renderAttemptId} · {pendingReview.decision} · {pendingReview.reason}
                    </p>
                    <Button
                        disabled={busy || !!autosave.conflict}
                        onClick={() =>
                            void run(async () => {
                                await autosave.flush({
                                    recover: true,
                                    update: (value) => value,
                                    accept: (value, remote) => ({ ...value, selections: remote.selections }),
                                    write: async () => {
                                        const prior = await readPendingReview();
                                        if (prior) await api.getAdvertisingReviewForOperation(projectId, p, prior.clientOperationId);
                                        return readProduction(true);
                                    },
                                });
                            })
                        }
                    >
                        查询原验收
                    </Button>
                    {reviewMissing && (
                        <Button
                            disabled={busy || !!autosave.conflict}
                            onClick={() =>
                                void run(async () => {
                                    await submitReview(pendingReview);
                                })
                            }
                        >
                            重试原验收提交
                        </Button>
                    )}
                    <details>
                        <summary>候选完整标识</summary>
                        {pendingReview.submissionId}
                    </details>
                    <p>未确认前普通保存不会提交该决定；新编辑保留。</p>
                </section>
            )}
            {pendingCutIssue && (
                <section>
                    <p>原成片问题待核对：{pendingCutIssue.note}</p>
                    <Button disabled={busy} onClick={() => void cutIssue().catch((e) => setError(String(e)))}>
                        查询原成片问题
                    </Button>
                    {cutIssueMissing && (
                        <Button disabled={busy} onClick={() => void cutIssue(undefined, true).catch((e) => setError(String(e)))}>
                            重试原成片问题
                        </Button>
                    )}
                </section>
            )}
            {autosave.conflict && <Button onClick={() => void run(() => autosave.refreshConflict())}>重新读取服务端制作</Button>}
            {remote && (
                <section role="alert">
                    <p>制作状态冲突，本地设置保留。服务端修订 {remote.revision}，请对照后选择。</p>
                    <details>
                        <summary>查看服务端设置</summary>
                        <pre className="ad-json">{JSON.stringify(remote.value, null, 2)}</pre>
                    </details>
                    <Button
                        onClick={() => {
                            autosave.resolveConflict(remote.value);
                        }}
                    >
                        采用服务端设置
                    </Button>
                    <Button
                        onClick={() => {
                            autosave.resolveConflict(settings);
                        }}
                    >
                        保留本地设置继续编辑
                    </Button>
                </section>
            )}
            <div hidden={!!stage && stage !== "preview"}>
                <fieldset disabled={busy || !!autosave.conflict} className="ad-production-grid">
                    <label hidden={!shots.some((s) => s.voiceover.trim())}>
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
                    <label hidden={!shots.some((s) => s.voiceover.trim())}>
                        默认音色
                        <select value={settings.voiceId} onChange={(e) => setSettings({ ...settings, voiceId: e.target.value })}>
                            <option value="">选择音色</option>
                            {voices.map((v) => (
                                <option key={v}>{v}</option>
                            ))}
                        </select>
                    </label>
                    <p>{shots.some((s) => s.voiceover.trim()) ? "默认值仅用于新生成；不会修改已有候选或已采用旁白。" : "无旁白，不需要配置 TTS 模型。"}</p>
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
                    <Button
                        onClick={() =>
                            void autosave.flush().catch((e) => {
                                if (current()) setError(String(e));
                            })
                        }
                    >
                        保存制作设置
                    </Button>
                </fieldset>
                {settings.musicResourceId && <audio controls src={resourceFileUrl(settings.musicResourceId)} aria-label="背景音乐试听" />}
            </div>
            <div hidden={!!stage && stage !== "shots"}>
                <AdvertisingReviewPanel
                    projectId={projectId}
                    production={production}
                    current={current}
                    onBusy={onShotBusy}
                    onRework={(issue, submission) => {
                        setRework({ issue, submission });
                        onStageChange?.("shots");
                    }}
                />
                {shots.map((shot, index) => (
                    <AdvertisingShotGeneration
                        key={shot.id}
                        shot={shot}
                        index={index}
                        models={models}
                        protocols={protocols.data || []}
                        submissions={submissions.data?.items || []}
                        production={production}
                        projectId={projectId}
                        rework={rework}
                        disabled={busy || !!autosave.conflict}
                        current={current}
                        onBusy={onShotBusy}
                        flush={() => autosave.flush()}
                        review={review}
                    />
                ))}
                <PaginationBar current={page} pageSize={20} total={submissions.data?.total || 0} pageSizeOptions={[20]} onChange={setPage} />
                <Button onClick={() => void submissions.refetch()}>刷新候选历史</Button>
            </div>
            <div hidden={!!stage && stage !== "preview"}>
                <fieldset disabled={busy || !!autosave.conflict || !fitReady}>
                    <label>
                        画幅适配
                        <select value={fit} onChange={(e) => setSettings({ ...settings, fit: e.target.value as "pad" | "crop" })}>
                            <option value="pad">留边，保留完整商品</option>
                            <option value="crop">裁切铺满，可能裁掉边缘</option>
                        </select>
                    </label>
                    {shots.map((shot, index) => {
                        const selection = settings.selections[shot.id];
                        return (
                            selection?.videoSubmissionId && (
                                <label key={shot.id}>
                                    镜头{index + 1}裁剪起点（毫秒）
                                    <input
                                        aria-label={"镜头" + (index + 1) + "裁剪起点（毫秒）"}
                                        type="number"
                                        min={0}
                                        step={1}
                                        value={selection.videoStartMs}
                                        onChange={(e) => {
                                            const value = e.target.valueAsNumber;
                                            if (Number.isSafeInteger(value) && value >= 0) setSettings({ ...settings, selections: { ...settings.selections, [shot.id]: { ...selection, videoStartMs: value } } });
                                        }}
                                    />
                                </label>
                            )
                        );
                    })}
                    <Button
                        disabled={!complete || dirty}
                        onClick={() =>
                            void run(async () => {
                                await createAssembly();
                            })
                        }
                    >
                        组装新时间线
                    </Button>
                    <Button
                        disabled={!shots.length || !complete || !fitReady}
                        onClick={() =>
                            void run(async () => {
                                if (!shots.length || !complete || remote) return;
                                const synced = await autosave.flush();
                                if (!current()) return;
                                const signature = JSON.stringify({ fit: synced.value.fit, selections: synced.value.selections, musicResourceId: synced.value.musicResourceId, musicVolume: synced.value.musicResourceId ? synced.value.musicVolume : 0 });
                                if (!fitReady) return;
                                const raw = await storage.current.getItem(roughCutStorageKey);
                                const pending = raw ? (JSON.parse(raw) as { signature: string; assemblyId: string }) : undefined;
                                let target = pending?.signature === signature ? await api.getAdvertisingAssembly(projectId, p, pending.assemblyId) : undefined;
                                if (!target?.current) {
                                    let page = 1;
                                    for (;;) {
                                        const history = await api.listAdvertisingAssemblies(projectId, p, page, 100);
                                        target = history.items.find((a) => a.current);
                                        if (target || page * 100 >= history.total) break;
                                        page++;
                                    }
                                }
                                if (!target?.current) target = await createAssembly();
                                if (!current()) return;
                                await storage.current.setItem(roughCutStorageKey, JSON.stringify({ signature, assemblyId: target.id, fit }));
                                if (!current()) return;
                                setAssembly(target);
                                await createRender(target);
                                await storage.current.removeItem(roughCutStorageKey);
                            })
                        }
                    >
                        一键生成粗剪
                    </Button>
                    {dirty && <p>准备粗剪会先保存当前制作设置；不会调用模型。</p>}
                </fieldset>
            </div>
            <div hidden={!!stage && stage !== "preview" && stage !== "export"}>
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
                        <h3>当前查看的组装</h3>
                        {!shownAssembly?.current && <p role="status">{shownAssembly?.staleReason || "历史组装来源不完整；当前性未知"}。历史文件仍可查看。</p>}
                        <details>
                            <summary>组装来源技术标识</summary>组装 {assembly.id} · 来源制作修订 {assembly.sourceProductionRevision} · 原选择哈希 {assembly.selectionHash}
                        </details>
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
                                        await createRender(assembly);
                                    })
                                }
                            >
                                导出原组装
                            </Button>
                            {assembly.canvasId ? <Link to={"/canvas/" + encodeURIComponent(assembly.canvasId)}>打开高级时间线（独立编辑分支）</Link> : <span>画布已删除；原组装仍保留。</span>}
                        </div>
                        <p>本地FFmpeg导出不调用模型、不扣模型积分，但占用服务器资源。手改画布的导出由任务中心管理，不覆盖原组装。</p>
                        {renders.data?.items.map((r) => (
                            <div key={r.id}>
                                <p>
                                    导出任务 {r.task.id} · {r.task.status} · {r.task.stage} {r.task.error}
                                </p>
                                <Button disabled={!r.available || !r.resourceId || busy} onClick={() => setSelectedOutput(r)}>
                                    审看此输出
                                </Button>
                                {r.available && r.task.status === "succeeded" && r.resourceId && (
                                    <a href={resourceFileUrl(r.resourceId)} download>
                                        {shownAssembly?.current ? "下载成片MP4" : "下载历史版本 MP4"}
                                    </a>
                                )}
                            </div>
                        ))}
                        <AdvertisingCutReview
                            key={assembly.id}
                            assembly={shownAssembly!}
                            render={download}
                            disabled={busy || !!pendingReview || !!remote || autosave.dirty || assemblyState.isFetching}
                            onReview={async (intent) => {
                                if (working.current || !current()) throw new Error("操作进行中或制作已切换");
                                working.current = true;
                                setBusy(true);
                                try {
                                    await submitReview(intent);
                                    const refreshed = await assemblyState.refetch();
                                    if (refreshed.error) throw refreshed.error;
                                } finally {
                                    working.current = false;
                                    if (current()) setBusy(false);
                                }
                            }}
                            onRecheck={async () => {
                                const a = await assemblyState.refetch();
                                const r = await renders.refetch();
                                if (a.error || r.error) throw a.error || r.error;
                            }}
                            onIssue={async (atMs, note) => {
                                if (!download) return;
                                await cutIssue({ clientOperationId: crypto.randomUUID(), assemblyId: assembly.id, renderAttemptId: download.id, atMs, note, category: "other" });
                            }}
                        />
                        <PaginationBar current={renderPage} pageSize={20} total={renders.data?.total || 0} pageSizeOptions={[20]} onChange={setRenderPage} />
                    </section>
                )}
            </div>
            {actionTarget &&
                createPortal(
                    stage === "shots" ? (
                        <Button type="primary" onClick={() => onStageChange?.("preview")}>
                            查看成片预览
                        </Button>
                    ) : stage === "preview" ? (
                        <Button
                            type="primary"
                            disabled={!complete || dirty || busy || !!remote || !fitReady}
                            onClick={() =>
                                void run(async () => {
                                    await createAssembly();
                                })
                            }
                        >
                            组装此版本时间线
                        </Button>
                    ) : stage === "export" ? (
                        <Button type="primary" onClick={() => onStageChange?.("preview")}>
                            返回成片验收与交付
                        </Button>
                    ) : null,
                    actionTarget,
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
