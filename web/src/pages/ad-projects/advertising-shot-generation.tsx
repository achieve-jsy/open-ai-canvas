import { advertisingCandidateLabel } from "./advertising-candidate-label";
import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "antd";
import { AppModal } from "@/components/ui/product/app-modal";
import { PaginationBar } from "@/components/layout/workspace-page";
import type { AdvertisingShot } from "@/services/api/advertising-project";
import type { PublicChannelModel } from "@/services/api/logical-models";
import type { ModelProtocolDefinition } from "@/lib/model-protocols";
import { videoDurationOptions, type VideoCapabilityConfig } from "@/lib/model-capabilities";
import { getActiveUserScope } from "@/lib/user-scope";
import { localForageStorageForScope } from "@/lib/localforage-storage";
import { ApiError } from "@/services/api/request";
import * as api from "@/services/api/advertising-generation";
import { AdvertisingCandidateStudio, type CandidateDecision } from "./advertising-candidate-studio";

type PendingBatch = { projectId: string; productionId: string; request: api.AdvertisingBatchRequest; batchId?: string; executing?: boolean; sourceIssueId?: string };
export function AdvertisingShotGeneration({
    shot,
    index,
    models,
    protocols,
    submissions,
    production,
    projectId,
    disabled,
    current,
    flush,
    review,
    onBusy,
    rework,
}: {
    shot: AdvertisingShot;
    index: number;
    models: PublicChannelModel[];
    protocols: ModelProtocolDefinition[];
    submissions: api.AdvertisingSubmission[];
    production: api.AdvertisingProductionDetail;
    projectId: string;
    disabled: boolean;
    current: () => boolean;
    flush: () => Promise<unknown>;
    review: (s: api.AdvertisingSubmission, decision: CandidateDecision) => Promise<void>;
    onBusy?: (id: string, busy: boolean) => void;
    rework?: { issue: api.AdvertisingIssue; submission: api.AdvertisingSubmission };
}) {
    const client = useQueryClient();
    const p = production.production.id;
    const scope = getActiveUserScope();
    const storage = localForageStorageForScope(scope);
    const storageKey = "advertising-batch:" + projectId + ":" + p + ":" + shot.id;
    const key = ["advertising-candidates", scope, projectId, p];
    const [videoModel, setVideoModel] = useState("");
    const [audioModelOverride, setAudioModel] = useState<string>();
    const [voiceOverride, setVoice] = useState<string>();
    const audioModel = audioModelOverride ?? production.settings.ttsModelId;
    const voice = voiceOverride ?? production.settings.voiceId;
    const [format, setFormat] = useState("mp3");
    const [counts, setCounts] = useState({ video: 1, audio: 1 });
    const [adjustment, setAdjustment] = useState("");
    const [sourceIssueId, setSourceIssueId] = useState<string>();
    const contextKey = "advertising-rework:" + projectId + ":" + p + ":" + shot.id;
    const [busy, setBusy] = useState(false);
    const working = useRef(false);
    const [error, setError] = useState("");
    const [pending, setPending] = useState<PendingBatch>();
    const [ready, setReady] = useState(false);
    const [missing, setMissing] = useState(false);
    const [recovered, setRecovered] = useState<api.AdvertisingBatchDetail>();
    const [page, setPage] = useState(1);
    const [confirm, setConfirm] = useState<api.AdvertisingBatchDetail>();
    const [approved, setApproved] = useState<string[]>([]);
    const [now, setNow] = useState(Date.now());
    const activity = useQuery({ queryKey: [...key, "activity"], queryFn: ({ signal }) => api.getAdvertisingActivity(projectId, p, signal), retry: false, refetchInterval: 4000 });
    const history = useQuery({ queryKey: [...key, "batches", shot.id, page], queryFn: ({ signal }) => api.listAdvertisingBatches(projectId, p, page, 20, signal), retry: false, refetchInterval: 4000 });
    const blocked = (kind: "video" | "audio") => !ready || !!pending || activity.isPending || !!activity.error || !!activity.data?.items.some((s) => s.shotId === shot.id && s.kind === kind);
    const refresh = async () => {
        await Promise.all([client.invalidateQueries({ queryKey: key }), client.invalidateQueries({ queryKey: ["advertising-production"] })]);
    };
    useEffect(() => {
        onBusy?.(shot.id, busy || !!pending);
    }, [busy, pending, shot.id, onBusy]);
    useEffect(() => () => onBusy?.(shot.id, false), [shot.id, onBusy]);
    useEffect(() => {
        let active = true;
        void Promise.resolve(storage.getItem(storageKey))
            .then((raw) => {
                if (!active || !current()) return;
                if (raw) {
                    const saved = JSON.parse(raw) as PendingBatch;
                    if (
                        !saved ||
                        saved.projectId !== projectId ||
                        saved.productionId !== p ||
                        !saved.request ||
                        saved.request.shotId !== shot.id ||
                        typeof saved.request.clientOperationId !== "string" ||
                        !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(saved.request.clientOperationId) ||
                        ![1, 2, 3].includes(saved.request.count) ||
                        !["video", "audio"].includes(saved.request.kind) ||
                        typeof saved.request.modelId !== "string" ||
                        !saved.request.modelId.trim() ||
                        typeof saved.request.adjustment !== "string" ||
                        saved.request.adjustment.length > 1000 ||
                        (saved.request.kind === "audio" && (typeof saved.request.voiceId !== "string" || !["mp3", "wav"].includes(saved.request.format || ""))) ||
                        (saved.batchId !== undefined && (typeof saved.batchId !== "string" || !saved.batchId)) ||
                        (saved.executing !== undefined && typeof saved.executing !== "boolean") ||
                        (saved.sourceIssueId !== undefined && typeof saved.sourceIssueId !== "string")
                    )
                        throw new Error("原批次记录不完整，请人工核对");
                    saved.request = {
                        clientOperationId: saved.request.clientOperationId,
                        shotId: shot.id,
                        kind: saved.request.kind,
                        modelId: saved.request.modelId,
                        count: saved.request.count,
                        adjustment: saved.request.adjustment,
                        ...(saved.request.kind === "audio" ? { voiceId: saved.request.voiceId, format: saved.request.format } : {}),
                    };
                    setPending(saved);
                    setSourceIssueId(saved.sourceIssueId);
                }
                setReady(true);
            })
            .catch((e) => {
                if (active && current()) setError(String(e));
            });
        return () => {
            active = false;
        };
    }, [storageKey]);
    useEffect(() => {
        void Promise.resolve(storage.getItem(contextKey))
            .then((raw) => {
                if (!raw || !current()) return;
                const context = JSON.parse(raw);
                if (
                    context?.projectId !== projectId ||
                    context?.productionId !== p ||
                    context?.shotId !== shot.id ||
                    typeof context?.sourceIssueId !== "string" ||
                    !context.sourceIssueId ||
                    typeof context?.adjustment !== "string" ||
                    context.adjustment.length > 1000
                )
                    throw new Error("返工上下文不完整，请重新选择问题");
                setSourceIssueId(context.sourceIssueId);
                setAdjustment(context.adjustment);
            })
            .catch((e) => {
                if (current()) setError(String(e));
            });
    }, [contextKey]);
    useEffect(() => {
        if (!rework || rework.submission.shotId !== shot.id) return;
        setAdjustment(rework.issue.note);
        setSourceIssueId(rework.issue.id);
        void Promise.resolve(storage.setItem(contextKey, JSON.stringify({ projectId, productionId: p, shotId: shot.id, sourceIssueId: rework.issue.id, adjustment: rework.issue.note }))).catch((e) => {
            if (current()) setError(String(e));
        });
        setApproved([]);
        if (rework.submission.kind === "video") setVideoModel(rework.submission.publicRequest.modelId);
        else {
            setAudioModel(rework.submission.publicRequest.modelId);
            setVoice(rework.submission.publicRequest.voiceId || "");
            setFormat(rework.submission.publicRequest.format || "mp3");
        }
    }, [rework]);
    // New form parameters never silently re-authorize an old frozen request.
    useEffect(() => {
        setApproved([]);
    }, [videoModel, audioModel, voice, format, counts.video, counts.audio, adjustment]);
    useEffect(() => {
        if (!confirm) return;
        const timer = setInterval(() => setNow(Date.now()), 1000);
        return () => clearInterval(timer);
    }, [confirm]);
    const remember = async (value: PendingBatch) => {
        const raw = JSON.stringify(value);
        if (value.sourceIssueId) await storage.setItem(contextKey, JSON.stringify({ projectId, productionId: p, shotId: shot.id, sourceIssueId: value.sourceIssueId, adjustment: value.request.adjustment }));
        await storage.setItem(storageKey, raw);
        if ((await storage.getItem(storageKey)) !== raw) throw new Error("无法可靠保存原操作键，未发送请求");
        if (!current()) throw new Error("账号或制作已切换");
        setPending(value);
        setMissing(false);
    };
    const acknowledged = async (result: api.AdvertisingBatchDetail) => {
        if (!current()) throw new Error("账号或制作已切换");
        setRecovered(result);
        setPage(1);
        await storage.removeItem(storageKey);
        setPending(undefined);
        await refresh();
    };
    const run = async (fn: () => Promise<void>) => {
        if (working.current || disabled || !current()) return;
        working.current = true;
        setBusy(true);
        setError("");
        try {
            await fn();
        } catch (e) {
            if (current()) setError(e instanceof ApiError && e.reason === "advertising_quote_expired" ? "报价已过期，请重新获取报价" : e instanceof Error ? e.message : "操作失败");
        } finally {
            working.current = false;
            if (current()) setBusy(false);
        }
    };
    const recover = async (saved: PendingBatch) => {
        try {
            const result = saved.batchId ? await api.getAdvertisingBatch(projectId, p, saved.batchId) : await api.getAdvertisingBatchForOperation(projectId, p, saved.request.clientOperationId);
            await acknowledged(result);
            setConfirm(undefined);
            setApproved([]);
            setMissing(false);
            setError("已恢复原批次；当前表单未提交，新的参数需另行获取报价。");
        } catch (e) {
            if (e instanceof ApiError && (e.status === 404 || e.reason === "not_found")) setMissing(true);
            throw e;
        }
    };
    const prepare = (kind: "video" | "audio") =>
        run(async () => {
            if (blocked(kind)) return;
            await flush();
            if (!current()) return;
            const request: api.AdvertisingBatchRequest = {
                clientOperationId: crypto.randomUUID(),
                shotId: shot.id,
                kind,
                modelId: kind === "video" ? videoModel : audioModel,
                count: counts[kind] as 1 | 2 | 3,
                adjustment,
                ...(kind === "audio" ? { voiceId: voice, format } : {}),
            };
            const saved = { projectId, productionId: p, request, sourceIssueId };
            await remember(saved);
            try {
                await acknowledged(await api.prepareAdvertisingBatch(projectId, p, request));
            } catch (e) {
                if (e instanceof ApiError && !!e.reason && !e.retryable) {
                    await storage.removeItem(storageKey);
                    setPending(undefined);
                    throw e;
                }
                await recover(saved);
            }
        });
    const execute = () =>
        run(async () => {
            if (
                !confirm ||
                approved.length !== confirm.items.length ||
                confirm.items.length !== confirm.batch.count ||
                confirm.items.some((s) => Date.parse(s.quote.expiresAt) <= Date.now()) ||
                activity.data?.items.some((s) => s.shotId === shot.id && s.kind === confirm.batch.kind)
            )
                return;
            await flush();
            if (!current()) return;
            const first = confirm.items[0];
            const saved: PendingBatch = {
                projectId,
                productionId: p,
                request: {
                    clientOperationId: confirm.batch.clientOperationId,
                    shotId: shot.id,
                    kind: confirm.batch.kind,
                    modelId: first.publicRequest.modelId,
                    count: confirm.batch.count,
                    adjustment: confirm.batch.adjustment,
                    ...(confirm.batch.kind === "audio" ? { voiceId: first.publicRequest.voiceId, format: first.publicRequest.format } : {}),
                },
                batchId: confirm.batch.id,
                executing: true,
                sourceIssueId,
            };
            await remember(saved);
            setApproved([]);
            try {
                await acknowledged(
                    await api.executeAdvertisingBatch(
                        projectId,
                        p,
                        confirm.batch.id,
                        confirm.items.map((s) => ({ submissionId: s.id, quoteHash: s.quote.quoteHash })),
                    ),
                );
                setConfirm(undefined);
            } catch (e) {
                if (e instanceof ApiError && !!e.reason && !e.retryable) {
                    await storage.removeItem(storageKey);
                    setPending(undefined);
                    await refresh();
                    throw e;
                }
                await recover(saved);
            }
        });
    const model = models.find((m) => m.id === audioModel);
    const params = protocols.find((p) => p.value === model?.protocol)?.parameters || [];
    const voices = params.find((p) => ["audioVoice", "voice"].includes(p.name))?.values || [];
    const formats: string[] = (params.find((p) => ["audioFormat", "format", "response_format"].includes(p.name))?.values || []).filter((v) => v === "mp3" || v === "wav");
    const videos = models.filter((m) => {
        const v = m.capabilityConfig?.video as VideoCapabilityConfig | undefined;
        return m.available && m.capability === "video" && v && !v.processing && v.ratios?.includes("9:16") && videoDurationOptions(v).some((n) => n >= Math.ceil(shot.durationMs / 1000));
    });
    const batches = [...new Map([...(recovered ? [recovered] : []), ...(history.data?.items || [])].map((b) => [b.batch.id, b])).values()].filter((b) => b.batch.shotId === shot.id);
    const items = [...new Map([...batches.flatMap((b) => b.items), ...submissions, ...(activity.data?.items || [])].filter((s) => s.shotId === shot.id).map((s) => [s.id, s])).values()];
    return (
        <article className="ad-shot" aria-label={"镜头" + (index + 1) + "制作"}>
            <h3>
                镜头{index + 1} · {shot.durationMs / 1000} 秒
            </h3>
            <p>{shot.visual}</p>
            <p>
                屏幕文字：{shot.onScreenText || "无"} · 旁白：{shot.voiceover || "无"}
            </p>
            <fieldset disabled={disabled || busy} className="ad-production-grid">
                <label>
                    视频模型
                    <select aria-label={"镜头" + (index + 1) + "视频模型"} value={videoModel} onChange={(e) => setVideoModel(e.target.value)}>
                        <option value="">选择受管视频模型</option>
                        {videos.map((m) => (
                            <option key={m.id} value={m.id}>
                                {m.displayName}
                            </option>
                        ))}
                    </select>
                </label>
                <label>
                    视频候选数量
                    <select aria-label={"镜头" + (index + 1) + "视频候选数量"} value={counts.video} onChange={(e) => setCounts({ ...counts, video: Number(e.target.value) })}>
                        {[1, 2, 3].map((n) => (
                            <option key={n} value={n}>
                                {n} 个 · {n} 次独立请求
                            </option>
                        ))}
                    </select>
                </label>
                <Button disabled={!videos.some((m) => m.id === videoModel) || blocked("video")} onClick={() => void prepare("video")}>
                    获取视频报价
                </Button>
            </fieldset>
            {!videos.length && <p>没有支持竖屏及镜头时长的受管视频模型，请联系管理员。</p>}
            {shot.voiceover.trim() && (
                <fieldset disabled={disabled || busy} className="ad-production-grid">
                    <label>
                        旁白模型
                        <select
                            value={audioModel}
                            onChange={(e) => {
                                setAudioModel(e.target.value);
                                setVoice("");
                            }}
                        >
                            <option value="">选择受管TTS模型</option>
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
                        音色
                        <select value={voice} onChange={(e) => setVoice(e.target.value)}>
                            <option value="">选择音色</option>
                            {voices.map((v) => (
                                <option key={v}>{v}</option>
                            ))}
                        </select>
                    </label>
                    <label>
                        格式
                        <select value={format} onChange={(e) => setFormat(e.target.value)}>
                            {formats.map((v) => (
                                <option key={v}>{v}</option>
                            ))}
                        </select>
                    </label>
                    <label>
                        旁白候选数量
                        <select aria-label={"镜头" + (index + 1) + "旁白候选数量"} value={counts.audio} onChange={(e) => setCounts({ ...counts, audio: Number(e.target.value) })}>
                            {[1, 2, 3].map((n) => (
                                <option key={n} value={n}>
                                    {n} 个 · {n} 次独立请求
                                </option>
                            ))}
                        </select>
                    </label>
                    <Button disabled={!models.some((m) => m.id === audioModel && m.available && m.capability === "audio") || !voices.includes(voice) || !formats.includes(format) || blocked("audio")} onClick={() => void prepare("audio")}>
                        获取旁白报价
                    </Button>
                </fieldset>
            )}
            <label>
                本次候选调整
                <textarea maxLength={1000} aria-label={"镜头" + (index + 1) + "候选调整"} value={adjustment} onChange={(e) => setAdjustment(e.target.value)} />
            </label>
            {sourceIssueId && <p>来源问题 {sourceIssueId} · 仅作为本机返工上下文，建议可编辑；问题不会自动解决。</p>}
            <p>一个批次必须逐项确认全部费用；不同镜头独立提交，可能部分成功。新候选不会自动采用。</p>
            {activity.data?.items.some((s) => s.shotId === shot.id) && <p role="status">存在活动或结果待核对任务；包括历史分页外的任务。请查询原任务，不重复生成。</p>}
            {(error || history.error || activity.error) && <p role="alert">{error || String(history.error || activity.error)}</p>}
            {pending && (
                <section>
                    <p>
                        原操作：{pending.request.kind} · {pending.request.count} 个 · {pending.request.modelId}
                    </p>
                    <pre className="ad-json">{JSON.stringify(pending.request, null, 2)}</pre>
                    <Button disabled={busy} onClick={() => void run(() => recover(pending))}>
                        查询原批次
                    </Button>
                    {missing && !pending.executing && (
                        <Button
                            disabled={busy}
                            onClick={() =>
                                void run(async () => {
                                    await flush();
                                    if (!current()) return;
                                    try {
                                        await acknowledged(await api.prepareAdvertisingBatch(projectId, p, pending.request));
                                    } catch (e) {
                                        if (e instanceof ApiError && !!e.reason && !e.retryable) {
                                            await storage.removeItem(storageKey);
                                            setPending(undefined);
                                        }
                                        throw e;
                                    }
                                })
                            }
                        >
                            重试原报价请求
                        </Button>
                    )}
                    {missing && pending.executing && <p>原批次不可读取，禁止重建已批准任务；请人工核对。</p>}
                </section>
            )}
            {batches.map((b, i) => (
                <section className="ad-candidate" key={b.batch.id}>
                    <h4>
                        B-{b.batch.id.length > 16 ? b.batch.id.slice(0, 8) + b.batch.id.slice(-8) : b.batch.id} · {b.batch.kind === "video" ? "视频" : "旁白"} · {b.batch.count} 次独立请求
                    </h4>
                    <p>{b.batch.closedAt ? "已关闭未执行报价" : b.batch.approvedAt ? "已批准批次；逐项结果见候选，不重试已成功项" : "尚未授权"}</p>
                    <details>
                        <summary>批次标识与冻结调整</summary>
                        <p>{b.batch.id}</p>
                        <p>{b.batch.adjustment || "无调整"}</p>
                    </details>
                    {!b.batch.approvedAt && !b.batch.closedAt && (
                        <Button
                            disabled={disabled || busy || !!pending || activity.isPending || !!activity.error || activity.data?.items.some((s) => s.shotId === shot.id && s.kind === b.batch.kind)}
                            onClick={() => {
                                setApproved([]);
                                setNow(Date.now());
                                setConfirm(b);
                            }}
                        >
                            查看并确认批次
                        </Button>
                    )}
                </section>
            ))}
            <PaginationBar current={page} pageSize={20} total={history.data?.total || 0} pageSizeOptions={[20]} onChange={setPage} />
            <Button onClick={() => void refresh()}>刷新批次与活动任务</Button>
            <AdvertisingCandidateStudio projectId={projectId} production={production} shotId={shot.id} submissions={items} disabled={disabled || busy} current={current} review={review} onBusy={onBusy} refresh={refresh} />
            <AppModal
                rootClassName="ad-workspace"
                open={!!confirm}
                title="确认完整批次费用"
                okText="确认全部费用并生成"
                cancelText="暂不生成"
                confirmLoading={busy}
                okButtonProps={{ disabled: disabled || !!pending || !confirm || approved.length !== confirm.items.length || confirm.items.length !== confirm.batch.count || confirm.items.some((s) => Date.parse(s.quote.expiresAt) <= now) }}
                onCancel={() => {
                    if (!busy) {
                        setConfirm(undefined);
                        setApproved([]);
                    }
                }}
                onOk={() => void execute()}
            >
                {confirm && (
                    <>
                        <p>
                            {confirm.batch.count} 个候选 = {confirm.batch.count} 次独立模型请求。全部勾选才授权本批次；生成不代表采用。
                        </p>
                        <p>本批次预授权总上限 {confirm.items.reduce((sum, s) => sum + s.quote.amountMicrocredits, 0)} 微积分。上游费用未知，本地零积分也不代表上游免费；最终费用以账单为准。</p>
                        {confirm.items.map((s, i) => (
                            <section key={s.id}>
                                <label className="ad-check">
                                    <input type="checkbox" checked={approved.includes(s.id)} onChange={(e) => setApproved((prior) => (e.target.checked ? [...prior.filter((id) => id !== s.id), s.id] : prior.filter((id) => id !== s.id)))} />
                                    批准 {advertisingCandidateLabel(s.id)}：预授权上限 {s.quote.amountMicrocredits} 微积分
                                </label>
                                <p>有效至 {new Date(s.quote.expiresAt).toLocaleString()}</p>
                                <pre className="ad-json">{JSON.stringify(s.publicRequest, null, 2)}</pre>
                            </section>
                        ))}
                        {confirm.items.some((s) => Date.parse(s.quote.expiresAt) <= now) && <p role="alert">报价已过期，请关闭并重新获取报价。</p>}
                    </>
                )}
                {error && <p role="alert">{error}</p>}
            </AppModal>
        </article>
    );
}
