import { advertisingCandidateLabel } from "./advertising-candidate-label";
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "antd";
import { AppModal } from "@/components/ui/product/app-modal";
import { getActiveUserScope } from "@/lib/user-scope";
import { localForageStorageForScope } from "@/lib/localforage-storage";
import { getResource, resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import { parseBackendGenerationResult } from "@/services/api/generation-task";
import { recoverGenerationTaskMedia, queryFailedVideoProviderTask, refreshGenerationTaskStatus } from "@/services/api/task-center";
import { ApiError } from "@/services/api/request";
import * as api from "@/services/api/advertising-generation";

export type CandidateDecision = { decision: "approve" | "reject"; reason: string; clearSelected?: boolean };
export function AdvertisingCandidateStudio({
    projectId,
    production,
    shotId,
    submissions,
    disabled,
    current,
    review,
    onBusy,
    refresh,
}: {
    projectId: string;
    production: api.AdvertisingProductionDetail;
    shotId: string;
    submissions: api.AdvertisingSubmission[];
    disabled: boolean;
    current: () => boolean;
    review: (s: api.AdvertisingSubmission, decision: CandidateDecision) => Promise<void>;
    onBusy?: (id: string, busy: boolean) => void;
    refresh: () => Promise<unknown>;
}) {
    const shot = production.confirmedSnapshot.snapshot.draft.shots.find((s) => s.id === shotId)!;
    const selection = production.settings.selections[shotId];
    // Keep only this page plus selected/viewed identities across polling and pagination.
    const known = useRef(new Map<string, api.AdvertisingSubmission>());
    submissions.forEach((s) => {
        const prior = known.current.get(s.id);
        if (!prior?.task || (s.task && !(prior.task.status === "succeeded" && s.task.status !== "succeeded"))) known.current.set(s.id, s);
    });
    const [view, setView] = useState("");
    const [compare, setCompare] = useState("");
    const retained = new Set([...submissions.map((s) => s.id), selection?.videoSubmissionId, selection?.audioSubmissionId, view, compare]);
    known.current.forEach((_, id) => {
        if (!retained.has(id)) known.current.delete(id);
    });
    const selectedIds = [selection?.videoSubmissionId, selection?.audioSubmissionId].filter((id): id is string => !!id);
    const missingSelected = selectedIds.filter((id) => !known.current.has(id));
    const selectedHistory = useQuery({
        queryKey: ["advertising-selected", getActiveUserScope(), projectId, production.production.id, ...missingSelected],
        enabled: !!missingSelected.length,
        retry: false,
        queryFn: async ({ signal }) => {
            const found: api.AdvertisingSubmission[] = [];
            let page = 1;
            while (!signal.aborted) {
                const result = await api.listAdvertisingSubmissions(projectId, production.production.id, page, 100, signal);
                found.push(...result.items.filter((s) => missingSelected.includes(s.id) && s.shotId === shotId));
                if (found.length === missingSelected.length || page * 100 >= result.total || !result.items.length) return found;
                page++;
            }
            return found;
        },
    });
    selectedHistory.data?.forEach((s) => {
        if (selectedIds.includes(s.id)) known.current.set(s.id, s);
    });
    const candidates = [...known.current.values()].filter((s) => !!s.task);
    // ponytail: scan paginated review history for latest per-candidate facts; a server summary can replace this if history grows large.
    const decisions = useQuery({
        queryKey: ["advertising-candidates", getActiveUserScope(), projectId, production.production.id, "candidate-reviews"],
        retry: false,
        refetchInterval: 4000,
        queryFn: async ({ signal }) => {
            const rows: api.AdvertisingReview[] = [];
            let page = 1;
            while (!signal.aborted) {
                const result = await api.listAdvertisingReviews(projectId, production.production.id, page, 100, signal);
                rows.push(...result.items);
                if (page * 100 >= result.total || !result.items.length) return rows;
                page++;
            }
            return rows;
        },
    });
    const [time, setTime] = useState(0);
    const players = useRef(new Map<string, HTMLMediaElement>());
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const working = useRef(false);
    const [target, setTarget] = useState<{ submission: api.AdvertisingSubmission; mode: "review" | "issue" }>();
    const [reason, setReason] = useState("");
    const [category, setCategory] = useState("product");
    const [at, setAt] = useState("");
    const [clear, setClear] = useState(false);
    const p = production.production.id;
    const scope = getActiveUserScope();
    const storage = localForageStorageForScope(scope);
    const issueKey = "advertising-issue:" + projectId + ":" + p + ":" + shotId;
    useEffect(() => {
        onBusy?.(shotId + ":review", busy);
    }, [busy, shotId, onBusy]);
    useEffect(() => () => onBusy?.(shotId + ":review", false), [shotId, onBusy]);
    const activeView = view || selection?.videoSubmissionId || candidates.find((s) => s.kind === "video" && s.task?.status === "succeeded")?.id || "";
    const selected = (s: api.AdvertisingSubmission) => (s.kind === "video" ? selection?.videoSubmissionId === s.id : selection?.audioSubmissionId === s.id);
    const run = async (fn: () => Promise<void>) => {
        if (working.current || disabled || !current()) return;
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
    const open = (s: api.AdvertisingSubmission, mode: "review" | "issue") => {
        setTarget({ submission: s, mode });
        setReason("");
        setAt("");
        setClear(false);
        setError("");
    };
    const decide = (decision: "approve" | "reject") =>
        run(async () => {
            if (!target || !reason.trim() || (decision === "reject" && selected(target.submission) && !clear)) return;
            await review(target.submission, { decision, reason, ...(decision === "reject" ? { clearSelected: clear } : {}) });
            if (current()) {
                setTarget(undefined);
                await refresh();
            }
        });
    const [pendingIssue, setPendingIssue] = useState<api.AdvertisingIssueRequest>();
    const [issueMissing, setIssueMissing] = useState(false);
    const readPendingIssue = async () => {
        const raw = await storage.getItem(issueKey);
        if (!raw) return;
        const saved = JSON.parse(raw);
        const request = saved?.request;
        if (
            saved?.projectId !== projectId ||
            saved?.productionId !== p ||
            saved?.shotId !== shotId ||
            !request ||
            typeof request.clientOperationId !== "string" ||
            !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(request.clientOperationId) ||
            typeof request.submissionId !== "string" ||
            !request.submissionId ||
            typeof request.note !== "string" ||
            !request.note.trim() ||
            request.note.length > 2000 ||
            typeof request.category !== "string" ||
            !request.category.trim() ||
            request.category.length > 24 ||
            (request.atMs !== undefined && (!Number.isSafeInteger(request.atMs) || request.atMs < 0))
        )
            throw new Error("原问题记录不完整，需人工核对");
        const original: api.AdvertisingIssueRequest = { clientOperationId: request.clientOperationId, submissionId: request.submissionId, note: request.note, category: request.category, ...(request.atMs !== undefined ? { atMs: request.atMs } : {}) };
        if (current()) setPendingIssue(original);
        return original;
    };
    useEffect(() => {
        void readPendingIssue().catch((e) => {
            if (current()) setError(String(e));
        });
    }, [issueKey]);
    const recoverIssue = async (request: api.AdvertisingIssueRequest) => {
        try {
            await api.getAdvertisingIssueForOperation(projectId, p, request.clientOperationId);
        } catch (e) {
            if (current() && e instanceof ApiError && (e.status === 404 || e.reason === "not_found")) setIssueMissing(true);
            throw e;
        }
        if (!current()) return;
        await storage.removeItem(issueKey);
        setPendingIssue(undefined);
        setIssueMissing(false);
        await refresh();
        setError("已恢复原问题；当前编辑未另行保存，请核对后再次点击保存问题。");
    };
    const sendIssue = async (request: api.AdvertisingIssueRequest) => {
        if (!current()) return;
        try {
            await api.createAdvertisingIssue(projectId, p, request);
        } catch (e) {
            if (e instanceof ApiError && !!e.reason && !e.retryable) {
                await storage.removeItem(issueKey);
                if (current()) setPendingIssue(undefined);
                throw e;
            }
            await recoverIssue(request);
            return;
        }
        if (!current()) return;
        await storage.removeItem(issueKey);
        setPendingIssue(undefined);
        setIssueMissing(false);
        await refresh();
        setError("原问题已保存；新的编辑不会随恢复请求提交。");
        return true;
    };
    const saveIssue = () =>
        run(async () => {
            if (!target || !reason.trim()) return;
            const prior = await readPendingIssue();
            if (prior) {
                await recoverIssue(prior);
                return;
            }
            const request: api.AdvertisingIssueRequest = { clientOperationId: crypto.randomUUID(), submissionId: target.submission.id, category, note: reason, ...(at !== "" ? { atMs: Number(at) } : {}) };
            if (at !== "" && (!Number.isSafeInteger(Number(at)) || Number(at) < 0)) throw new Error("问题时间必须为非负整数毫秒");
            const value = JSON.stringify({ projectId, productionId: p, shotId, request });
            await storage.setItem(issueKey, value);
            if ((await storage.getItem(issueKey)) !== value || !current()) throw new Error("无法保存问题操作键，未发送");
            setPendingIssue(request);
            if ((await sendIssue(request)) && current()) setTarget(undefined);
        });
    const control = async (action: "play" | "pause" | "seek") => {
        setError("");
        try {
            const media = [activeView, compare]
                .filter(Boolean)
                .map((id) => players.current.get(id))
                .filter((v): v is HTMLMediaElement => !!v);
            if (action === "seek" && (!Number.isFinite(time) || time < 0 || media.some((m) => !Number.isFinite(m.duration) || time > m.duration))) throw new Error("定位时间超出可播放范围，等待视频元数据后重试");
            if (action === "play") await Promise.all(media.map((m) => m.play()));
            else
                media.forEach((m) => {
                    m.pause();
                    if (action === "seek") m.currentTime = time;
                });
        } catch (e) {
            setError(e instanceof Error ? e.message : "播放失败，请使用原生播放器重试");
        }
    };
    const productId = production.confirmedSnapshot.snapshot.draft.productResourceIds[0];
    return (
        <section aria-label={"镜头候选工作台 " + shotId}>
            <h4>候选工作台</h4>
            <p>当前查看不等于已采用。人工验收视频区间固定为 0–{shot.durationMs}ms；已采用项保留原剪裁，不重复批准归零。</p>
            {productId && <img className="ad-product-image" src={resourceFileUrl(productId)} alt="商品原图对照" />}
            <div className="ad-production-grid">
                <label>
                    当前查看
                    <select value={activeView} onChange={(e) => setView(e.target.value)}>
                        <option value="">选择视频候选</option>
                        {candidates
                            .filter((s) => s.kind === "video")
                            .map((s, i) => (
                                <option key={s.id} value={s.id}>
                                    {advertisingCandidateLabel(s.id)}
                                    {selected(s) ? " · 已采用" : ""}
                                </option>
                            ))}
                    </select>
                </label>
                <label>
                    对比视频
                    <select value={compare} onChange={(e) => setCompare(e.target.value)}>
                        <option value="">不对比</option>
                        {candidates
                            .filter((s) => s.kind === "video" && s.id !== activeView)
                            .map((s, i) => (
                                <option key={s.id} value={s.id}>
                                    {advertisingCandidateLabel(s.id)} · {s.publicRequest.modelId}
                                </option>
                            ))}
                    </select>
                </label>
            </div>
            <div className="ad-actions">
                <Button onClick={() => void control("play")}>播放对比视频</Button>
                <Button onClick={() => void control("pause")}>暂停对比视频</Button>
                <label>
                    对比定位（秒）
                    <input aria-label="对比定位秒" type="number" min={0} step={0.1} value={time} onChange={(e) => setTime(e.target.valueAsNumber)} />
                </label>
                <Button onClick={() => void control("seek")}>定位对比视频</Button>
            </div>
            <p>同步播放与定位仅辅助审片，不保证逐帧对齐。</p>
            {(error || selectedHistory.error || decisions.error) && <p role="alert">{error || String(selectedHistory.error || decisions.error)}</p>}
            {pendingIssue && (
                <section>
                    <p>
                        原问题：{pendingIssue.category} · {pendingIssue.note} · 候选 {advertisingCandidateLabel(pendingIssue.submissionId!)}
                    </p>
                    <details>
                        <summary>候选完整标识</summary>
                        {pendingIssue.submissionId}
                    </details>
                    <Button disabled={disabled || busy} onClick={() => void run(() => recoverIssue(pendingIssue))}>
                        查询原问题
                    </Button>
                    {issueMissing && (
                        <Button
                            disabled={disabled || busy}
                            onClick={() =>
                                void run(async () => {
                                    await sendIssue(pendingIssue);
                                })
                            }
                        >
                            重试原问题提交
                        </Button>
                    )}
                </section>
            )}
            <div className="ad-candidate-grid">
                {candidates.map((s, i) => (
                    <Candidate
                        key={s.id}
                        submission={s}
                        label={advertisingCandidateLabel(s.id)}
                        decision={decisions.data?.filter((r) => r.submissionId === s.id).sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0]}
                        targetMs={shot.durationMs}
                        selected={selected(s)}
                        start={selection?.videoStartMs || 0}
                        shown={s.kind === "audio" || selected(s) || s.id === activeView || s.id === compare}
                        disabled={disabled || busy}
                        player={(el) => {
                            if (el) players.current.set(s.id, el);
                            else players.current.delete(s.id);
                        }}
                        onReview={() => open(s, "review")}
                        onIssue={() => open(s, "issue")}
                        recover={(action) =>
                            run(async () => {
                                try {
                                    await action();
                                } finally {
                                    await refresh();
                                }
                            })
                        }
                    />
                ))}
            </div>
            {!candidates.length && <p>尚无已执行候选。先获取报价，逐项授权后生成。</p>}
            <AppModal
                rootClassName="ad-workspace"
                open={!!target}
                title={target?.mode === "issue" ? "记录候选问题" : "候选人工验收"}
                footer={null}
                onCancel={() => {
                    if (!busy) setTarget(undefined);
                }}
            >
                {target && (
                    <>
                        <p>
                            候选 {advertisingCandidateLabel(target.submission.id)} · {target.submission.kind === "video" ? "验收并采用区间 0–" + shot.durationMs + "ms" : "完整旁白"}
                        </p>
                        <details>
                            <summary>候选完整标识</summary>
                            {target.submission.id}
                        </details>
                        <label>
                            {target.mode === "issue" ? "问题说明" : "验收理由"}
                            <textarea aria-label={target.mode === "issue" ? "问题说明" : "验收理由"} maxLength={2000} value={reason} onChange={(e) => setReason(e.target.value)} />
                        </label>
                        {target.mode === "issue" ? (
                            <>
                                <label>
                                    问题类别
                                    <select value={category} onChange={(e) => setCategory(e.target.value)}>
                                        <option value="product">商品一致性</option>
                                        <option value="motion">动作与画面</option>
                                        <option value="text">文字</option>
                                        <option value="audio">声音</option>
                                        <option value="other">其他</option>
                                    </select>
                                </label>
                                <label>
                                    时间点（ms，可空）
                                    <input aria-label="问题时间点" type="number" min={0} step={1} value={at} onChange={(e) => setAt(e.target.value)} />
                                </label>
                                <Button disabled={disabled || busy || !reason.trim()} onClick={() => void saveIssue()}>
                                    保存问题
                                </Button>
                                <p>只保存问题，不发起模型请求；返工另行授权。</p>
                            </>
                        ) : (
                            <>
                                <p>确认商品、画面、文字与声音符合要求；技术成功不代表人工验收通过。</p>
                                {selected(target.submission) && (
                                    <label className="ad-check">
                                        <input type="checkbox" checked={clear} onChange={(e) => setClear(e.target.checked)} />
                                        拒绝会清除当前选用，历史媒体保留
                                    </label>
                                )}
                                <div className="ad-actions">
                                    {!selected(target.submission) && (
                                        <Button type="primary" disabled={disabled || busy || !reason.trim()} onClick={() => void decide("approve")}>
                                            批准并选用
                                        </Button>
                                    )}
                                    <Button disabled={disabled || busy || !reason.trim() || (selected(target.submission) && !clear)} onClick={() => void decide("reject")}>
                                        淘汰候选
                                    </Button>
                                </div>
                            </>
                        )}
                        {error && <p role="alert">{error}</p>}
                    </>
                )}
            </AppModal>
        </section>
    );
}
function Candidate({
    submission: s,
    label,
    decision,
    targetMs,
    selected,
    start,
    shown,
    disabled,
    player,
    onReview,
    onIssue,
    recover,
}: {
    submission: api.AdvertisingSubmission;
    label: string;
    decision?: api.AdvertisingReview;
    targetMs: number;
    selected: boolean;
    start: number;
    shown: boolean;
    disabled: boolean;
    player: (el: HTMLMediaElement | null) => void;
    onReview: () => void;
    onIssue: () => void;
    recover: (action: () => Promise<unknown>) => Promise<void>;
}) {
    let resourceId = "";
    try {
        if (s.task?.status === "succeeded") {
            const result = parseBackendGenerationResult(s.task);
            resourceId = resourceIdFromStorageKey((s.kind === "video" ? result.video : result.audio || result.audios?.[0])?.storageKey) || "";
        }
    } catch {
        /* Malformed results are unavailable, not approval evidence. */
    }
    const media = useQuery({ queryKey: ["advertising-candidate", getActiveUserScope(), s.productionId, s.id, resourceId], queryFn: () => getResource(resourceId), enabled: !!resourceId, retry: false });
    const [unreadable, setUnreadable] = useState(false);
    const duration = media.data?.durationMs || 0;
    const valid = !!resourceId && !unreadable && media.data?.status === "ready" && media.data.mimeType.startsWith(s.kind + "/") && duration > 0 && (s.kind === "video" ? duration >= targetMs : duration <= targetMs);
    const task = s.task!;
    const providerQuery = task.status === "failed" && !task.mediaStage && (task.type?.startsWith("canvas_video") || task.type?.startsWith("video_")) && !!task.providerRequestId;
    return (
        <article className="ad-candidate" aria-label={label + "候选"}>
            <h5>
                {label} · {s.kind === "video" ? "视频" : "旁白"}{" "}
                {selected ? "· 已采用" : decision?.decision === "reject" ? "· 已淘汰" : decision?.decision === "approve" ? "· 曾批准，非当前采用" : task.status === "succeeded" ? "· 待验收" : "· " + task.status}
            </h5>
            <p>
                {s.publicRequest.modelId} {s.publicRequest.voiceId}
            </p>
            {decision && <p>最近验收：{decision.reason}</p>}
            <div hidden={!shown}>
                {resourceId &&
                    (s.kind === "video" ? (
                        <video ref={player} controls muted preload="metadata" src={resourceFileUrl(resourceId)} onError={() => setUnreadable(true)} aria-label={label + "视频候选预览"} />
                    ) : (
                        <audio ref={player} controls preload="metadata" src={resourceFileUrl(resourceId)} onError={() => setUnreadable(true)} aria-label={label + "旁白候选试听"} />
                    ))}
            </div>
            <p>
                实际时长 {duration || "未知"}ms · 目标 {targetMs}ms{selected && s.kind === "video" ? " · 已选区间 " + start + "–" + (start + targetMs) + "ms" : ""}
            </p>
            {task.status === "succeeded" && !valid && <p role="status">资源不可用、处理中、无法播放或时长不合要求；不能批准。可重新检查资源。</p>}
            <details>
                <summary>技术标识与检查</summary>
                <p>
                    候选 {s.id} · 批次 {s.batchId || "历史单项"} · 任务 {task.id}
                </p>
                <p>
                    任务 {task.status} · 媒体 {task.resultState || media.data?.status || "尚无可用资源"} · 恢复能力 {task.canRecoverMedia ? "可重存" : "不可重存"}
                </p>
            </details>
            <div className="ad-actions">
                <Button disabled={disabled || (!valid && !selected)} onClick={onReview}>
                    {selected ? "复核或淘汰已选" : "验收此候选"}
                </Button>
                <Button disabled={disabled || !resourceId || duration <= 0} onClick={onIssue}>
                    记录问题
                </Button>
                {resourceId && (
                    <Button
                        disabled={disabled}
                        onClick={() => {
                            setUnreadable(false);
                            void media.refetch();
                        }}
                    >
                        重新检查资源
                    </Button>
                )}
                {task.canRecoverMedia && (
                    <Button disabled={disabled} onClick={() => void recover(() => recoverGenerationTaskMedia(task.id))}>
                        重存原任务媒体
                    </Button>
                )}
                {providerQuery && (
                    <Button disabled={disabled} onClick={() => void recover(() => queryFailedVideoProviderTask(task.id))}>
                        查询原上游任务
                    </Button>
                )}
                <Button disabled={disabled} onClick={() => void recover(() => refreshGenerationTaskStatus(task.id))}>
                    刷新原任务
                </Button>
            </div>
            {task.status === "failed" && !providerQuery && !task.canRecoverMedia && <p>无可用自动恢复能力。结果未知时先人工核对；确认终态后才可重新获取报价、重新授权。</p>}
        </article>
    );
}
