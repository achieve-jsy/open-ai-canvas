import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useBlocker, useParams } from "react-router";
import { Button } from "antd";
import { canonicalize } from "json-canonicalize";
import { WorkspacePage, PageHeader, PaginationBar } from "@/components/layout/workspace-page";
import { AppModal } from "@/components/ui/product/app-modal";
import { useUserStore } from "@/stores/use-user-store";
import { getActiveUserScope, scopedLocalStorage } from "@/lib/user-scope";
import { ApiError } from "@/services/api/request";
import { ADVERTISING_REASONS } from "@/services/api/error-codes";
import * as api from "@/services/api/advertising-project";
import { AdvertisingDraftForm } from "./advertising-draft-form";
import { AdvertisingAssets } from "./advertising-assets";
import { AdvertisingProduction } from "./advertising-production";
import { useAdvertisingAutosave, advertisingSaveLabel } from "./use-advertising-autosave";
import { AdvertisingWorkbench, type AdvertisingStage } from "./advertising-workbench";
import { resourceFileUrl } from "@/services/api/resources";
import "./advertising.css";

function validCachedDraft(value: unknown): value is { name: string; draft: api.AdvertisingDraft } {
    const v = value as { name?: unknown; draft?: api.AdvertisingDraft } | null;
    const d = v?.draft;
    const strings = (items: unknown): items is string[] => Array.isArray(items) && items.every((item) => typeof item === "string");
    return (
        !!v &&
        typeof v.name === "string" &&
        !!d &&
        d.schemaVersion === 1 &&
        !!d.brief &&
        [d.brief.productName, d.brief.description, d.brief.audience, d.brief.cta].every((item) => typeof item === "string") &&
        strings(d.brief.sellingPoints) &&
        Number.isFinite(d.brief.durationMs) &&
        (d.brief.goal === undefined || d.brief.goal === "awareness" || d.brief.goal === "conversion") &&
        !!d.brand &&
        [d.brand.name, d.brand.primaryColor, d.brand.logoResourceId].every((item) => typeof item === "string") &&
        strings(d.productResourceIds) &&
        strings(d.additionalResourceIds) &&
        Array.isArray(d.shots) &&
        d.shots.every((shot) => !!shot && [shot.id, shot.visual, shot.voiceover, shot.onScreenText].every((item) => typeof item === "string") && Number.isFinite(shot.durationMs) && strings(shot.referenceResourceIds))
    );
}

export default function AdvertisingDetailPage() {
    const { id } = useParams();
    const userId = useUserStore((s) => s.user?.id);
    return id && userId ? <AdvertisingEditor key={userId + ":" + id} id={id} userId={userId} /> : <p role="alert">请先登录并选择广告项目。</p>;
}
export function AdvertisingEditor({ id, userId }: { id: string; userId: string }) {
    const client = useQueryClient();
    const key = ["advertising", userId, id];
    const query = useQuery({ queryKey: key, queryFn: ({ signal }) => api.getAdvertisingProject(id, signal), refetchOnWindowFocus: false, retry: false });
    const [saved, setSaved] = useState<api.AdvertisingDetail>();
    const [busy, setBusy] = useState(false);
    const [uploading, setUploading] = useState(false);
    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");
    const [confirmOpen, setConfirmOpen] = useState(false);
    const [reviewed, setReviewed] = useState<{ revision: number; name: string; draft: api.AdvertisingDraft }>();
    const [stage, setStage] = useState<AdvertisingStage>("brief");
    const [actionTarget, setActionTarget] = useState<HTMLDivElement | null>(null);
    const [activeRevision, setActiveRevision] = useState<string>();
    const [enterProduction, setEnterProduction] = useState(false);
    const [productionState, setProductionState] = useState({ unsafe: false, status: "尚未进入制作", selected: 0, total: 0, assembly: false, downloadable: false });
    const onProductionState = useCallback((state: typeof productionState) => setProductionState(state), []);
    const [historyId, setHistoryId] = useState("");
    const [page, setPage] = useState(1);
    const operationKey = `ad-script-plan:${id}`;
    const [operationId, setOperationId] = useState(() => scopedLocalStorage.getItem(operationKey) || "");
    const [planError, setPlanError] = useState("");
    const [starting, setStarting] = useState(false);
    const [recoveryUntil, setRecoveryUntil] = useState(() => (operationId ? Date.now() + 12000 : 0));
    const live = useRef(true);
    const writing = useRef(false);
    const scope = useRef(getActiveUserScope());
    useEffect(() => {
        live.current = true;
        return () => {
            live.current = false;
        };
    }, []);
    const current = () => live.current && getActiveUserScope() === scope.current && useUserStore.getState().user?.id === userId;
    const remember = (data: api.AdvertisingDetail) => {
        if (!current()) return;
        setSaved(data);
        client.setQueryData(key, data);
    };
    const editable = (data: api.AdvertisingDetail) => ({ revision: data.revision, value: { name: data.project.name, draft: data.draft } });
    const autosave = useAdvertisingAutosave({
        scopeKey: userId + ":" + id + ":draft",
        current,
        validate: validCachedDraft,
        initial: query.data && editable(query.data),
        save: async (value, expectedRevision) => {
            const result = await api.saveAdvertisingProject(id, { ...value, expectedRevision });
            remember(result);
            return editable(result);
        },
        read: async () => {
            const result = await api.getAdvertisingProject(id);
            if (current()) setSaved(result);
            return editable(result);
        },
    });
    const draft = autosave.value?.draft;
    const name = autosave.value?.name || "";
    const setDraft = (draft: api.AdvertisingDraft) => {
        setNotice("");
        autosave.setValue((value) => value && { ...value, draft });
    };
    const setName = (name: string) => {
        setNotice("");
        autosave.setValue((value) => value && { ...value, name });
    };
    const dirty = autosave.dirty;
    const conflict = !!autosave.conflict;
    const remote = autosave.conflict?.remote;
    useEffect(() => {
        if (query.data && current() && (!saved || query.data.revision >= saved.revision)) setSaved(query.data);
    }, [query.data]);
    const plan = useQuery({
        queryKey: [...key, "script-plan", operationId],
        queryFn: ({ signal }) => (operationId ? api.getAdvertisingScriptPlan(id, operationId, signal) : api.getLatestAdvertisingScriptPlan(id, signal)),
        retry: false,
        refetchInterval: (query) => (query.state.data?.status === "running" || (!query.state.data && recoveryUntil > Date.now()) ? 1500 : false),
    });
    const candidate = plan.data;
    useEffect(() => {
        if (!activeRevision && saved?.latestConfirmedRevisionId) setActiveRevision(saved.latestConfirmedRevisionId);
    }, [activeRevision, saved?.latestConfirmedRevisionId]);
    useEffect(() => {
        if (candidate) {
            setPlanError("");
            setRecoveryUntil(0);
        }
    }, [candidate]);
    const adoptedPlan = !!candidate?.draft && JSON.stringify(candidate.draft) === JSON.stringify(draft);
    const stalePlan = !!candidate && !!saved && candidate.sourceRevision !== saved.revision && !adoptedPlan;
    const readyProductImage = draft?.productResourceIds.length === 1 && saved?.resources.some((item) => item.id === draft.productResourceIds[0] && item.available && item.mimeType.startsWith("image/"));
    const canGenerate =
        !!saved &&
        !!draft?.brief.description.trim() &&
        draft.brief.durationMs === 15000 &&
        readyProductImage &&
        !dirty &&
        !busy &&
        !uploading &&
        !conflict &&
        !starting &&
        ((!operationId && plan.isSuccess && !candidate) || candidate?.status === "ready" || candidate?.status === "blocked" || candidate?.status === "failed");
    const generate = async (retryId?: string) => {
        if ((!retryId && !canGenerate) || !saved || !current() || dirty || starting) return;
        const nextId = retryId || crypto.randomUUID();
        scopedLocalStorage.setItem(operationKey, nextId);
        setOperationId(nextId);
        setPlanError("");
        setStarting(true);
        setRecoveryUntil(Date.now() + 12000);
        try {
            const synced = await autosave.flush();
            if (!current()) return;
            setStage("script");
            const result = await api.createAdvertisingScriptPlan(id, { expectedRevision: synced.revision, clientOperationId: nextId });
            if (current()) {
                client.setQueryData([...key, "script-plan", nextId], result);
                setRecoveryUntil(0);
            }
        } catch (e) {
            if (!current()) return;
            if (e instanceof ApiError && (e.status === undefined || e.status === 408 || e.status >= 500)) {
                setPlanError("提交结果未确认，正在读取服务端进度。请勿重复提交。");
                try {
                    const latestPlan = await api.getLatestAdvertisingScriptPlan(id);
                    if (current() && latestPlan?.operationId === nextId) {
                        client.setQueryData([...key, "script-plan", nextId], latestPlan);
                        setPlanError("");
                        setRecoveryUntil(0);
                    }
                } catch {
                    // The same operation ID remains available for bounded GET recovery and idempotent retry.
                }
            } else {
                scopedLocalStorage.removeItem(operationKey);
                setOperationId("");
                setRecoveryUntil(0);
                setPlanError(e instanceof Error ? e.message : "生成脚本失败");
                if (e instanceof ApiError && (e.status === 409 || e.reason === ADVERTISING_REASONS.revisionConflict)) {
                    await readRemote();
                }
            }
        } finally {
            if (current()) setStarting(false);
        }
    };
    const guard = autosave.unsafe || busy || uploading || productionState.unsafe;
    const blocker = useBlocker(guard);
    useEffect(() => {
        const unload = (event: BeforeUnloadEvent) => {
            if (guard) {
                event.preventDefault();
                event.returnValue = "";
            }
        };
        window.addEventListener("beforeunload", unload);
        return () => window.removeEventListener("beforeunload", unload);
    }, [guard]);
    const revisions = useQuery({ queryKey: [...key, "revisions", page], queryFn: ({ signal }) => api.listAdvertisingRevisions(id, page, 20, signal), enabled: !!saved, retry: false });
    const latest = useQuery({
        queryKey: [...key, "history", saved?.latestConfirmedRevisionId],
        queryFn: ({ signal }) => api.getAdvertisingRevision(id, saved!.latestConfirmedRevisionId!, signal),
        enabled: !!saved?.latestConfirmedRevisionId,
        retry: false,
    });
    const differsFromConfirmed = !!latest.data && (name !== latest.data.snapshot.name || JSON.stringify(draft) !== JSON.stringify(latest.data.snapshot.draft));
    const history = useQuery({ queryKey: [...key, "history", historyId], queryFn: ({ signal }) => api.getAdvertisingRevision(id, historyId, signal), enabled: !!historyId, retry: false });
    const readRemote = async () => {
        try {
            await autosave.refreshConflict();
        } catch (e) {
            if (current()) setError("读取服务端版本失败；本地输入仍保留。" + (e instanceof Error ? e.message : ""));
        }
    };
    const write = async (confirm: boolean) => {
        if (!saved || !draft || writing.current || uploading || !current()) return;
        writing.current = true;
        if (confirm) setBusy(true);
        setError("");
        setNotice("");
        try {
            const synced = await autosave.flush();
            if (!current()) return;
            if (confirm) {
                if (!reviewed || synced.revision !== reviewed.revision || canonicalize(synced.value) !== canonicalize({ name: reviewed.name, draft: reviewed.draft })) throw new Error("草稿修订已变化，请关闭弹窗并重新核对脚本。");
                const remote = await api.getAdvertisingProject(id);
                if (!current()) return;
                if (remote.revision !== reviewed.revision) {
                    remember(remote);
                    throw new Error("服务端修订已变化，请关闭弹窗并重新核对脚本。");
                }
                const result = await api.confirmAdvertisingProject(id, reviewed.revision);
                if (!current()) return;
                // Same saved content may reuse an older immutable snapshot; revision alone is not its identity.
                if (!result.snapshot.id || result.snapshot.projectId !== id || result.snapshot.snapshot.name !== reviewed.name || canonicalize(result.snapshot.snapshot.draft) !== canonicalize(reviewed.draft))
                    throw new Error("返回的确认并非本次核对版本，请重新核对。");
                remember({ ...remote, revision: result.currentRevision, latestConfirmedRevisionId: result.snapshot.id });
                client.setQueryData([...key, "history", result.snapshot.id], result.snapshot);
                void query.refetch();
                setActiveRevision(result.snapshot.id);
                setEnterProduction(true);
                setStage("shots");
                setNotice("脚本已确认并冻结，正在创建或恢复此版本制作；不会生成视频。");
                setConfirmOpen(false);
                await client.invalidateQueries({ queryKey: [...key, "revisions"] });
            } else {
                setNotice("已保存到服务端");
            }
            void client.invalidateQueries({ queryKey: ["advertising-list", userId] });
        } catch (e) {
            if (!current()) return;
            setError(e instanceof Error ? e.message : "操作失败，输入已保留");
            if (e instanceof ApiError && e.reason === ADVERTISING_REASONS.revisionConflict) {
                await readRemote();
            }
        } finally {
            writing.current = false;
            if (current()) setBusy(false);
        }
    };
    const openReview = () => {
        if (!draft || !autosave.revision || dirty || busy || uploading || conflict || productionState.unsafe) return;
        setError("");
        setReviewed({ revision: autosave.revision, name, draft: structuredClone(draft) });
        setConfirmOpen(true);
    };
    const productionStage = stage === "shots" || stage === "preview" || stage === "export";
    if (autosave.restored && !saved && draft)
        return (
            <WorkspacePage className="ad-workspace">
                <PageHeader title="本机恢复的广告草稿" />
                <p role="status">{advertisingSaveLabel[autosave.status]}。尚未读取服务端版本，不能确认或生成。</p>
                <Button onClick={() => void query.refetch()}>重新连接服务端</Button>
                <AdvertisingDraftForm value={draft} onChange={setDraft} />
            </WorkspacePage>
        );
    return (
        <WorkspacePage className="ad-workspace">
            <PageHeader title={name || "广告制作工作台"} actions={<Link to="/ad-projects">返回广告项目</Link>} />
            {!autosave.restored || !saved || !draft || autosave.status === "loading" ? (
                <div role={query.error ? "alert" : "status"}>
                    {query.error ? String(query.error) : "正在读取广告…"}
                    {query.error && <Button onClick={() => void query.refetch()}>重试</Button>}
                </div>
            ) : (
                <AdvertisingWorkbench
                    stage={stage}
                    onStageChange={setStage}
                    actionRef={setActionTarget}
                    facts={{
                        brief: readyProductImage && draft.brief.description.trim() ? "素材与场景已填写" : "待补充素材与场景",
                        script: differsFromConfirmed ? "草稿有修改 · 待确认" : latest.data ? "已有确认快照" : "待人工确认",
                        shots: productionState.total ? `已采用 ${productionState.selected} / ${productionState.total} 镜头` : "待进入制作",
                        preview: productionState.assembly ? "已有组装 · 审片状态见成片" : "尚无组装",
                        export: productionState.downloadable ? "指定输出可下载 · 交付状态见成片" : "待生成可用文件",
                    }}
                    summary={
                        <>
                            <section className="ad-section">
                                <h2>PROJECT / 项目概览</h2>
                                {saved.resources
                                    .filter((r) => draft.productResourceIds.includes(r.id) && r.available && r.mimeType.startsWith("image/"))
                                    .slice(0, 1)
                                    .map((r) => (
                                        <img key={r.id} className="ad-summary-image" src={resourceFileUrl(r.id)} alt={r.title || "商品参考图"} />
                                    ))}
                                <strong>{draft.brief.productName || name}</strong>
                                <dl className="ad-summary-list">
                                    <div>
                                        <dt>目标时长</dt>
                                        <dd>{draft.brief.durationMs / 1000} 秒</dd>
                                    </div>
                                    <div>
                                        <dt>画面比例</dt>
                                        <dd>9:16 竖屏</dd>
                                    </div>
                                    <div>
                                        <dt>分镜</dt>
                                        <dd>{draft.shots.length} 个</dd>
                                    </div>
                                    <div>
                                        <dt>声音</dt>
                                        <dd>{draft.shots.some((s) => s.voiceover.trim()) ? "含旁白" : "无旁白"}</dd>
                                    </div>
                                </dl>
                            </section>
                            <section className="ad-section">
                                <h2>保存与版本</h2>
                                <p role="status">
                                    草稿修订 {autosave.revision} · {advertisingSaveLabel[autosave.status]}
                                </p>
                                <p role="status">制作：{productionState.status}</p>
                                <p>{differsFromConfirmed ? "当前草稿与最新确认版本不同；旧制作仍来自已确认脚本。" : latest.data ? `确认来源：草稿修订 ${latest.data.sourceRevision}` : "当前脚本尚未确认"}</p>
                                <p>脚本规划使用系统视觉文字模型；可用性以服务端检查结果为准，不代表视频模型已就绪。</p>
                            </section>
                        </>
                    }
                    primary={
                        !productionStage ? (
                            stage === "brief" ? (
                                <Button
                                    type="primary"
                                    disabled={!canGenerate}
                                    loading={starting}
                                    onClick={() => {
                                        setStage("script");
                                        void generate();
                                    }}
                                >
                                    生成广告脚本
                                </Button>
                            ) : (
                                <Button type="primary" disabled={dirty || busy || uploading || conflict || productionState.unsafe} onClick={openReview}>
                                    确认脚本，下一步
                                </Button>
                            )
                        ) : undefined
                    }
                >
                    <section className="ad-section">
                        <label>
                            项目名称
                            <input aria-label="项目名称" maxLength={120} value={name} disabled={busy || uploading} onChange={(e) => setName(e.target.value)} />
                        </label>
                        <div className="flex flex-wrap gap-3 items-center mt-3">
                            <span role="status">
                                修订 {autosave.revision} · {advertisingSaveLabel[autosave.status]}
                            </span>
                            <Button loading={busy} disabled={uploading || conflict} onClick={() => void write(false)}>
                                保存草稿
                            </Button>
                            <details>
                                <summary>高级操作 · 手工编辑与重新规划</summary>
                                <Button disabled={!canGenerate} loading={starting} onClick={() => void generate()}>
                                    生成脚本
                                </Button>
                                {!readyProductImage && <span role="status">生成需恰好绑定一张已就绪的商品图。</span>}
                                <Button disabled={dirty || busy || uploading || conflict || productionState.unsafe} onClick={openReview}>
                                    确认脚本
                                </Button>
                            </details>
                            {draft.brief.durationMs !== 15000 && <p role="status">AI 规划仅支持 15 秒；当前时长可继续手工编写并确认。</p>}
                            {dirty && <span>先保存，才能确认。</span>}
                        </div>
                    </section>
                    {error && <p role="alert">{error}</p>}
                    {!operationId && planError && <p role="alert">{planError}</p>}
                    {notice && <p role="status">{notice}</p>}
                    {conflict && (
                        <section className="ad-section" aria-label="版本冲突">
                            <h2>服务端已有新版本，本地输入未覆盖</h2>
                            <p>请对照内容后选择。保留本地会以新的修订号再次保存，不会自动覆盖。</p>
                            <Button disabled={busy} onClick={() => void readRemote()}>
                                重新读取服务端版本
                            </Button>
                            {remote && (
                                <>
                                    <p>
                                        服务端修订 {remote.revision} · {remote.value.name}
                                    </p>
                                    <details>
                                        <summary>查看服务端内容</summary>
                                        <pre className="whitespace-pre-wrap break-all">{JSON.stringify(remote.value.draft, null, 2)}</pre>
                                    </details>
                                    <Button
                                        disabled={busy}
                                        onClick={() => {
                                            if (window.confirm("放弃本地修改，采用服务端版本？")) autosave.resolveConflict(remote.value);
                                        }}
                                    >
                                        采用服务端版本
                                    </Button>
                                    <Button
                                        disabled={busy}
                                        onClick={() => {
                                            autosave.resolveConflict(autosave.value!);
                                            setError("");
                                        }}
                                    >
                                        保留本地内容继续编辑
                                    </Button>
                                </>
                            )}
                        </section>
                    )}
                    <div className={stage === "brief" ? "ad-brief-grid" : undefined}>
                        <div hidden={stage !== "brief"}>
                            <AdvertisingAssets value={draft} onChange={setDraft} resources={saved.resources} disabled={busy} onBusy={setUploading} />
                        </div>
                        <div hidden={productionStage}>
                            <AdvertisingDraftForm value={draft} onChange={setDraft} resources={saved.resources} mode={stage === "brief" ? "brief" : "script"} disabled={busy || uploading} />
                        </div>
                    </div>
                    {(operationId || candidate || plan.error) && (
                        <section className="ad-section" aria-label="候选脚本" hidden={stage !== "script"}>
                            <h2>候选脚本</h2>
                            {starting || (!candidate && !plan.error) ? <p role="status">正在读取生成结果…</p> : null}
                            {candidate?.status === "running" && <p role="status">正在生成脚本…</p>}
                            {candidate?.status === "blocked" && <p role="alert">生成受阻；请检查场景与素材后重试。</p>}
                            {candidate?.status === "failed" && <p role="alert">{candidate.error || "生成失败，请重试。"}</p>}
                            {adoptedPlan && <p role="status">建议已进入当前草稿，尚需人工确认；保存不会再次生成脚本。</p>}
                            {stalePlan && (
                                <p role="alert">
                                    候选脚本基于修订 {candidate?.sourceRevision}，当前修订为 {saved.revision}；请重新生成。
                                </p>
                            )}
                            {!candidate && planError && <p role="alert">{recoveryUntil > Date.now() ? planError : "提交结果仍未确认；自动读取已暂停，可重试同一操作。"}</p>}
                            {plan.error && <p role="alert">读取生成结果失败：{String(plan.error)}</p>}
                            {plan.error && <Button onClick={() => void plan.refetch()}>重试读取结果</Button>}
                            {!candidate && (planError || plan.error) && recoveryUntil <= Date.now() && (
                                <Button disabled={starting || dirty || conflict} onClick={() => void generate(operationId)}>
                                    重试提交同一操作
                                </Button>
                            )}
                            {candidate?.status === "ready" && candidate.draft && (
                                <>
                                    <p>
                                        来源修订 {candidate.sourceRevision} · {candidate.draft.brief.durationMs / 1000} 秒
                                    </p>
                                    <p>场景：{candidate.draft.brief.description}</p>
                                    {candidate.draft.brief.goal && <p>目标：{candidate.draft.brief.goal === "awareness" ? "品牌认知" : "引导转化"}</p>}
                                    {candidate.draft.brief.cta && <p>行动号召：{candidate.draft.brief.cta}</p>}
                                    <ol className="space-y-3">
                                        {candidate.draft.shots.map((shot, index) => (
                                            <li key={shot.id}>
                                                <h3>
                                                    镜头 {index + 1} · {shot.durationMs / 1000} 秒
                                                </h3>
                                                <p>画面：{shot.visual}</p>
                                                <p>旁白：{shot.voiceover || "无旁白"}</p>
                                                <p>字幕：{shot.onScreenText || "无字幕"}</p>
                                                {!!shot.referenceResourceIds.length && <p>参考素材：{shot.referenceResourceIds.map((resourceId) => saved.resources.find((resource) => resource.id === resourceId)?.title || resourceId).join("、")}</p>}
                                            </li>
                                        ))}
                                    </ol>
                                    {!!candidate.evidence?.length && (
                                        <>
                                            <h3>依据</h3>
                                            <ul>
                                                {candidate.evidence.map((item, index) => (
                                                    <li key={index}>
                                                        {item.claim} · {item.source}
                                                        {item.resourceId && ` · ${saved.resources.find((resource) => resource.id === item.resourceId)?.title || item.resourceId}`}
                                                    </li>
                                                ))}
                                            </ul>
                                        </>
                                    )}
                                    {!!candidate.uncertainties?.length && (
                                        <>
                                            <h3>待核实</h3>
                                            <ul>
                                                {candidate.uncertainties.map((item, index) => (
                                                    <li key={index}>{item}</li>
                                                ))}
                                            </ul>
                                        </>
                                    )}
                                    <Button
                                        disabled={dirty || busy || uploading || conflict || stalePlan || adoptedPlan}
                                        onClick={() => {
                                            if (dirty || stalePlan || !current()) return;
                                            setStage("script");
                                            setDraft(candidate.draft!);
                                            setNotice("候选脚本已采用到本地草稿；请保存后再确认。");
                                        }}
                                    >
                                        采用候选脚本
                                    </Button>
                                </>
                            )}
                            {(candidate?.status === "failed" || candidate?.status === "blocked" || stalePlan) && (
                                <Button disabled={!canGenerate} onClick={() => void generate()}>
                                    重新生成
                                </Button>
                            )}
                        </section>
                    )}
                    <div hidden={!productionStage}>
                        <AdvertisingProduction
                            key={activeRevision || saved.latestConfirmedRevisionId || "unconfirmed"}
                            projectId={id}
                            userId={userId}
                            confirmedRevisionId={activeRevision || saved.latestConfirmedRevisionId}
                            stage={stage}
                            autoEnter={enterProduction}
                            actionTarget={productionStage ? actionTarget : null}
                            onStateChange={onProductionState}
                            onStageChange={setStage}
                        />
                    </div>
                    <section className="ad-section">
                        <h2>确认历史</h2>
                        <p>确认快照只读，不包含临时签名URL。</p>
                        {differsFromConfirmed && <p role="status">当前草稿与最新确认版本不同；历史确认仍然有效。</p>}
                        {latest.error && <p role="alert">暂时无法比较最新确认版本，请稍后重试。</p>}
                        {revisions.error ? (
                            <p role="alert">{String(revisions.error)}</p>
                        ) : revisions.data?.items.length === 0 ? (
                            <p>尚未确认。保存脚本后点击“确认脚本”。</p>
                        ) : (
                            <ul>
                                {revisions.data?.items.map((v) => (
                                    <li key={v.id}>
                                        <Button onClick={() => setHistoryId(v.id)}>
                                            来源修订 {v.sourceRevision} · {new Date(v.createdAt).toLocaleString()}
                                        </Button>
                                        {v.id === saved.latestConfirmedRevisionId && "最新确认"}
                                    </li>
                                ))}
                            </ul>
                        )}
                        <PaginationBar current={page} pageSize={20} total={revisions.data?.total || 0} pageSizeOptions={[20]} onChange={setPage} />
                    </section>
                </AdvertisingWorkbench>
            )}
            <AppModal
                rootClassName="ad-workspace"
                open={confirmOpen}
                title="确认当前已保存脚本？"
                okText="确认并冻结"
                cancelText="继续编辑"
                confirmLoading={busy}
                onOk={() => void write(true)}
                onCancel={() => {
                    if (!busy) setConfirmOpen(false);
                }}
                okButtonProps={{ disabled: dirty || uploading || conflict || autosave.revision !== reviewed?.revision }}
            >
                <p>
                    请核对草稿修订 {reviewed?.revision}：{reviewed?.name}。确认后冻结此版本并创建或恢复制作，不会生成视频。
                </p>
                {reviewed && (
                    <ol>
                        {reviewed.draft.shots.map((shot, i) => (
                            <li key={shot.id}>
                                镜头 {i + 1} · {shot.durationMs / 1000} 秒：{shot.visual} · {shot.voiceover || "无旁白"} · {shot.onScreenText || "无字幕"}
                            </li>
                        ))}
                    </ol>
                )}
                {reviewed && autosave.revision !== reviewed.revision && <p role="alert">草稿修订已变化，请关闭弹窗并重新核对。</p>}
                {error && <p role="alert">{error}</p>}
            </AppModal>
            <AppModal rootClassName="ad-workspace" open={!!historyId} title="只读确认版本" footer={null} onCancel={() => setHistoryId("")} width={900}>
                {history.error ? (
                    <p role="alert">{String(history.error)}</p>
                ) : history.data ? (
                    <>
                        <h2>{history.data.snapshot.name}</h2>
                        <p>来源修订 {history.data.sourceRevision}</p>
                        <AdvertisingDraftForm value={history.data.snapshot.draft} onChange={() => {}} disabled />
                        <Button
                            disabled={productionState.unsafe}
                            onClick={() => {
                                setActiveRevision(history.data!.id);
                                setEnterProduction(true);
                                setStage("shots");
                                setHistoryId("");
                            }}
                        >
                            打开此确认版本制作
                        </Button>
                        <Button
                            disabled={busy || uploading || conflict}
                            onClick={() => {
                                setDraft(structuredClone(history.data!.snapshot.draft));
                                setHistoryId("");
                                setStage("script");
                                setNotice("历史内容已复制为当前新草稿；尚未确认，不改变历史快照。");
                            }}
                        >
                            复制为新草稿
                        </Button>
                        <h3>冻结资源</h3>
                        <ul>
                            {history.data.snapshot.resources.map((r) => (
                                <li key={r.id}>
                                    {r.title} · {r.mimeType} · {r.width}×{r.height} · {r.durationMs / 1000} 秒
                                </li>
                            ))}
                        </ul>
                    </>
                ) : (
                    <p role="status">读取确认版本…</p>
                )}
            </AppModal>
            <AppModal
                rootClassName="ad-workspace"
                open={blocker.state === "blocked"}
                title="离开广告编辑？"
                okText="放弃并离开"
                cancelText="继续编辑"
                onOk={() => blocker.state === "blocked" && blocker.proceed()}
                onCancel={() => blocker.state === "blocked" && blocker.reset()}
            >
                <p>未保存的输入会丢失；已发出的操作可能仍在服务端完成。回来后请重新读取。</p>
            </AppModal>
        </WorkspacePage>
    );
}
