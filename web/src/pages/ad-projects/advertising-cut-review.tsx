import { useEffect, useState } from "react";
import { Button } from "antd";
import type * as api from "@/services/api/advertising-generation";
import { resourceFileUrl } from "@/services/api/resources";
import { advertisingCandidateLabel } from "./advertising-candidate-label";
export type ReviewIntent = api.AdvertisingReviewRequest extends infer R ? (R extends api.AdvertisingReviewRequest ? Omit<R, "clientOperationId" | "expectedProductionRevision"> : never) : never;
export function AdvertisingCutReview({
    assembly,
    render,
    disabled,
    onReview,
    onRecheck,
    onIssue,
}: {
    assembly: api.AdvertisingAssembly;
    render?: api.AdvertisingRender;
    disabled: boolean;
    onReview: (intent: ReviewIntent) => Promise<void>;
    onRecheck: () => Promise<void>;
    onIssue: (atMs: number, note: string) => Promise<void>;
}) {
    const [reason, setReason] = useState("");
    const [note, setNote] = useState("");
    const [atMs, setAtMs] = useState(0);
    const [error, setError] = useState("");
    const [receipt, setReceipt] = useState("");
    useEffect(() => {
        setReason("");
        setNote("");
        setAtMs(0);
        setError("");
        setReceipt("");
    }, [assembly.id, render?.id, render?.resourceId]);
    const shots = assembly.timeline.clips.filter((clip) => clip.kind === "video" && clip.id.startsWith("video-")).sort((a, b) => a.startMs - b.startMs);
    const available = !!render?.available && render.task.status === "succeeded" && !!render.resourceId;
    const approved = assembly.latestReview;
    const accepted = approved?.checkpoint === "assembly" && approved.decision === "approve" && approved.assemblyId === assembly.id && approved.renderAttemptId === render?.id && approved.resourceId === render?.resourceId;
    const act = async (fn: () => Promise<void>) => {
        setError("");
        try {
            await fn();
        } catch (e) {
            setError(e instanceof Error ? e.message : "操作失败");
        }
    };
    const decide = (decision: "approve" | "reject", delivery = false) =>
        act(async () => {
            if (!render?.resourceId) return;
            await onReview(
                delivery
                    ? { checkpoint: "delivery", decision: "approve", assemblyId: assembly.id, renderAttemptId: render.id, resourceId: render.resourceId, acceptedReviewId: approved!.id, reason: reason.trim() || "确认交付已批准的同一成片" }
                    : { checkpoint: "assembly", decision, assemblyId: assembly.id, renderAttemptId: render.id, resourceId: render.resourceId, reason: reason.trim() },
            );
            setReceipt(delivery ? "交付确认已保存（同一输出）" : "成片验收已保存");
        });
    return (
        <section className="ad-cut-review">
            <h3>成片来源与验收</h3>
            <p role="status">
                {assembly.current ? "当前成片依赖未变化" : assembly.staleReason || "历史组装，来源未知"}。来源制作修订 {assembly.sourceProductionRevision} 仅用于追溯，不表示当前性。
            </p>
            <p>
                输出 {assembly.timeline.output?.width ?? "未知"} × {assembly.timeline.output?.height ?? "未知"} · {assembly.sourceSettings?.fit ?? assembly.timeline.output?.fit ?? "未知"}；以下为源媒体实测尺寸，不代表原素材达到输出清晰度。
            </p>
            {!assembly.sourceSettings ? (
                <p>历史来源详情不完整：未知，不从当前设置回填。</p>
            ) : (
                <>
                    <ul>
                        {assembly.sources?.map((source, index) => (
                            <li key={source.key + ":" + index}>
                                {source.key === assembly.sourceSettings?.musicResourceId
                                    ? "背景音乐"
                                    : assembly.timeline.clips.some((clip) => clip.kind === "image" && clip.directMedia?.storageKey === "resource:" + source.resourceId)
                                      ? "Logo"
                                      : advertisingCandidateLabel(source.key)}{" "}
                                · {source.width > 0 && source.height > 0 ? source.width + " × " + source.height : "尺寸未知"} · {source.durationMs}ms
                                <details>
                                    <summary>来源技术标识</summary>task {source.taskId || "不适用"} · resource {source.resourceId}
                                </details>
                            </li>
                        ))}
                    </ul>
                    <p>
                        音乐：{assembly.sourceSettings.musicResourceId || "无背景音乐"} · 实际混音音量 {assembly.sourceSettings.musicVolume}
                    </p>
                    <ul>
                        {shots.map((clip, index) => {
                            const s = assembly.sourceSettings!.selections[clip.id.slice("video-".length)];
                            return (
                                <li key={clip.id}>
                                    镜头 {index + 1}：
                                    {s ? (
                                        <>
                                            视频 {advertisingCandidateLabel(s.videoSubmissionId)} · 裁剪起点 {s.videoStartMs}ms · {s.audioSubmissionId ? "旁白 " + advertisingCandidateLabel(s.audioSubmissionId) : "无旁白，不需要 TTS"}
                                        </>
                                    ) : (
                                        "来源未知"
                                    )}
                                </li>
                            );
                        })}
                    </ul>
                </>
            )}
            {render ? (
                <>
                    <p>正在审看指定成片输出</p>
                    <details>
                        <summary>组装与输出技术标识</summary>组装 {assembly.id} · 尝试 {render.id} · 输出 {render.resourceId || "尚未产生"}
                    </details>
                    {render.resourceId && (
                        <video
                            key={render.id + ":" + render.resourceId}
                            aria-label="成片MP4"
                            controls
                            preload="metadata"
                            src={resourceFileUrl(render.resourceId)}
                            onError={() => setError("成片播放失败，请重新检查输出可用性")}
                            onTimeUpdate={(e) => setAtMs(Math.round(e.currentTarget.currentTime * 1000))}
                            style={{ width: "100%", maxHeight: "60vh" }}
                        />
                    )}
                    {!available && <p role="alert">输出当前不可用；请重新检查，不会自动生成或渲染。</p>}
                </>
            ) : (
                <p>请先准备粗剪并选择一个成功输出审看；时间线预览不是最终混音。</p>
            )}
            <Button disabled={disabled} onClick={() => void act(onRecheck)}>
                重新检查输出可用性
            </Button>
            {error && <p role="alert">{error}</p>}
            {receipt && <p role="status">{receipt}</p>}
            <label>
                成片验收理由
                <textarea aria-label="成片验收理由" value={reason} maxLength={2000} onChange={(e) => setReason(e.target.value)} />
            </label>
            <div className="ad-actions">
                <Button disabled={disabled || !available || !assembly.current || !reason.trim()} onClick={() => void decide("approve")}>
                    批准此成片
                </Button>
                <Button disabled={disabled || !available || !assembly.current || !reason.trim()} onClick={() => void decide("reject")}>
                    退回此成片
                </Button>
                <Button disabled={disabled || !available || !assembly.current || !accepted} onClick={() => void decide("approve", true)}>
                    确认交付此输出
                </Button>
            </div>
            {available && (
                <a href={resourceFileUrl(render!.resourceId!)} download onClick={() => setReceipt("下载读取可能失效；若失败请重新检查输出可用性，不必重新渲染")}>
                    {assembly.current ? "下载此输出 MP4" : "下载历史输出 MP4"}
                </a>
            )}
            <label>
                当前文件问题时间（毫秒）
                <input aria-label="成片问题时间" type="number" min={0} max={assembly.timeline.durationMs} step={1} value={atMs} onChange={(e) => setAtMs(e.target.valueAsNumber)} />
            </label>
            <label>
                成片问题
                <textarea aria-label="成片问题" value={note} maxLength={2000} onChange={(e) => setNote(e.target.value)} />
            </label>
            <Button
                disabled={disabled || !available || !note.trim() || !Number.isSafeInteger(atMs) || atMs < 0 || atMs > assembly.timeline.durationMs}
                onClick={() =>
                    void act(async () => {
                        await onIssue(atMs, note.trim());
                        setNote("");
                        setReceipt("问题已绑定当前组装与渲染尝试；未调用模型");
                    })
                }
            >
                记录此输出问题
            </Button>
        </section>
    );
}
