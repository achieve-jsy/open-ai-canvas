import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "antd";
import { AppModal } from "@/components/ui/product/app-modal";
import type { AdvertisingShot } from "@/services/api/advertising-project";
import type { PublicChannelModel } from "@/services/api/logical-models";
import type { ModelProtocolDefinition } from "@/lib/model-protocols";
import { videoDurationOptions, type VideoCapabilityConfig } from "@/lib/model-capabilities";
import { getResource, resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import { parseBackendGenerationResult } from "@/services/api/generation-task";
import * as api from "@/services/api/advertising-generation";
import { ApiError } from "@/services/api/request";
import { getActiveUserScope } from "@/lib/user-scope";
import { ADVERTISING_REASONS } from "@/services/api/error-codes";

export function AdvertisingShotGeneration({
    shot,
    index,
    models,
    protocols,
    submissions,
    selection,
    defaults,
    disabled,
    current,
    quote,
    execute,
    adopt,
}: {
    shot: AdvertisingShot;
    index: number;
    models: PublicChannelModel[];
    protocols: ModelProtocolDefinition[];
    submissions: api.AdvertisingSubmission[];
    selection?: api.AdvertisingSelection;
    defaults: { ttsModelId: string; voiceId: string };
    disabled: boolean;
    current: () => boolean;
    quote: (request: Omit<api.AdvertisingSubmissionRequest, "clientOperationId">) => Promise<api.AdvertisingSubmission>;
    execute: (s: api.AdvertisingSubmission) => Promise<void>;
    adopt: (s: api.AdvertisingSubmission, start: number) => Promise<void>;
}) {
    const [videoModel, setVideoModel] = useState("");
    const [audioModel, setAudioModel] = useState(defaults.ttsModelId);
    const [voice, setVoice] = useState(defaults.voiceId);
    const [format, setFormat] = useState("mp3");
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const working = useRef(false);
    const [confirm, setConfirm] = useState<api.AdvertisingSubmission>();
    const model = models.find((m) => m.id === audioModel);
    const params = protocols.find((p) => p.value === model?.protocol)?.parameters || [];
    const voices = params.find((p) => ["audioVoice", "voice"].includes(p.name))?.values || [];
    const formats: string[] = (params.find((p) => ["audioFormat", "format", "response_format"].includes(p.name))?.values || []).filter((v) => v === "mp3" || v === "wav");
    const videos = models.filter((m) => {
        const v = m.capabilityConfig?.video as VideoCapabilityConfig | undefined;
        return m.available && m.capability === "video" && v && !v.processing && v.ratios?.includes("9:16") && videoDurationOptions(v).some((n) => n >= Math.ceil(shot.durationMs / 1000));
    });
    const run = async (fn: () => Promise<void>) => {
        if (working.current || disabled || !current()) return;
        working.current = true;
        setBusy(true);
        setError("");
        try {
            await fn();
        } catch (e) {
            if (current())
                setError(
                    e instanceof ApiError && e.reason === ADVERTISING_REASONS.quoteExpired
                        ? "报价已过期，请关闭后获取新报价"
                        : e instanceof ApiError && e.reason === ADVERTISING_REASONS.quoteChanged
                          ? "报价已变化，请重新获取并确认"
                          : e instanceof Error
                            ? e.message
                            : "操作失败",
                );
        } finally {
            working.current = false;
            if (current()) setBusy(false);
        }
    };
    const prepare = (kind: "video" | "audio") =>
        run(async () => {
            await quote({ shotId: shot.id, kind, modelId: kind === "video" ? videoModel : audioModel, voiceId: kind === "audio" ? voice : "", format: kind === "audio" ? format : "" });
            if (current()) setConfirm(undefined);
        });
    return (
        <article className="ad-shot" aria-label={"镜头" + (index + 1) + "制作"}>
            <h3>
                镜头{index + 1} · {shot.durationMs}ms
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
                <Button disabled={!videoModel} onClick={() => void prepare("video")}>
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
                    <Button disabled={!audioModel || !voices.includes(voice) || !formats.includes(format)} onClick={() => void prepare("audio")}>
                        获取旁白报价
                    </Button>
                    {!voices.length && <p>协议未提供可用音色；不能提交旁白生成。</p>}
                </fieldset>
            )}
            {error && <p role="alert">{error}。请求失败时保留操作键；请用相同选项重试或刷新恢复历史。</p>}
            {submissions
                .filter((s) => s.shotId === shot.id)
                .map((s) => (
                    <div className="ad-candidate" key={s.id}>
                        <p>
                            {s.kind === "video" ? "视频" : "旁白"} · {s.publicRequest.modelId} {s.publicRequest.voiceId} · {s.task?.status || "待确认报价"}
                        </p>
                        {s.task ? (
                            <>
                                <p>
                                    {s.task.stage} {s.task.error}
                                </p>
                                <AdvertisingCandidate
                                    submission={s}
                                    targetMs={shot.durationMs}
                                    selected={s.kind === "video" ? selection?.videoSubmissionId === s.id : selection?.audioSubmissionId === s.id}
                                    start={selection?.videoStartMs || 0}
                                    disabled={disabled || busy}
                                    onAdopt={(start) => run(() => adopt(s, start))}
                                />
                            </>
                        ) : (
                            <>
                                <p>
                                    预授权上限 {s.quote.amountMicrocredits} 微积分 · 有效至 {new Date(s.quote.expiresAt).toLocaleTimeString()}
                                </p>
                                <Button disabled={disabled || busy} onClick={() => setConfirm(s)}>
                                    查看并确认报价
                                </Button>
                            </>
                        )}
                    </div>
                ))}
            <AppModal
                rootClassName="ad-workspace"
                open={!!confirm}
                title="确认本镜头费用"
                okText="确认费用并生成"
                cancelText="暂不生成"
                confirmLoading={busy}
                okButtonProps={{ disabled: disabled || (!!confirm && Date.parse(confirm.quote.expiresAt) <= Date.now()) }}
                onCancel={() => {
                    if (!busy) setConfirm(undefined);
                }}
                onOk={() =>
                    confirm &&
                    void run(async () => {
                        await execute(confirm);
                        if (current()) setConfirm(undefined);
                    })
                }
            >
                {confirm && (
                    <>
                        <p>仅生成本镜头{confirm.kind === "video" ? "视频" : "旁白"}，不会自动采用。</p>
                        <p>预授权上限：{confirm.quote.amountMicrocredits} 微积分；最终费用以账单为准。</p>
                        <p>
                            模型：{confirm.publicRequest.modelId} · 音色：{confirm.publicRequest.voiceId || "不适用"}
                        </p>
                        <p>有效至 {new Date(confirm.quote.expiresAt).toLocaleString()}</p>
                        <pre className="ad-json">{JSON.stringify(confirm.publicRequest, null, 2)}</pre>
                        {Date.parse(confirm.quote.expiresAt) <= Date.now() && <p role="alert">报价已过期，请关闭后获取新报价。</p>}
                    </>
                )}
                {error && <p role="alert">{error}</p>}
            </AppModal>
        </article>
    );
}

function AdvertisingCandidate({ submission: s, targetMs, selected, start, disabled, onAdopt }: { submission: api.AdvertisingSubmission; targetMs: number; selected: boolean; start: number; disabled: boolean; onAdopt: (start: number) => Promise<void> }) {
    let resourceId = "";
    try {
        if (s.task?.status === "succeeded") {
            const result = parseBackendGenerationResult(s.task);
            resourceId = resourceIdFromStorageKey((s.kind === "video" ? result.video : result.audio || result.audios?.[0])?.storageKey) || "";
        }
    } catch {
        /* Invalid results are not adoptable. */
    }
    const media = useQuery({ queryKey: ["advertising-candidate", getActiveUserScope(), s.productionId, s.id, resourceId], queryFn: () => getResource(resourceId), enabled: !!resourceId, retry: false });
    const [trim, setTrim] = useState(selected ? start : 0);
    const [unreadable, setUnreadable] = useState(false);
    useEffect(() => {
        if (selected) setTrim(start);
    }, [selected, start]);
    if (s.task?.status !== "succeeded") return null;
    const duration = media.data?.durationMs || 0;
    const invalid =
        !resourceId || unreadable || media.data?.status !== "ready" || !media.data?.mimeType.startsWith(s.kind + "/") || duration <= 0 || !Number.isSafeInteger(trim) || trim < 0 || (s.kind === "video" ? duration - trim < targetMs : duration > targetMs);
    return (
        <div>
            {resourceId &&
                (s.kind === "video" ? (
                    <video controls preload="metadata" src={resourceFileUrl(resourceId)} onError={() => setUnreadable(true)} aria-label="视频候选预览" />
                ) : (
                    <audio controls preload="metadata" src={resourceFileUrl(resourceId)} onError={() => setUnreadable(true)} aria-label="旁白候选试听" />
                ))}
            <p>
                实际 {duration || "未知"}ms · 目标 {targetMs}ms {selected && "· 已采用"}
            </p>
            {s.kind === "video" && (
                <label>
                    裁剪起点（ms）
                    <input type="number" min={0} step={1} value={trim} disabled={disabled} onChange={(e) => setTrim(e.target.valueAsNumber)} />
                    <span>
                        拟用区间 {trim}–{trim + targetMs}ms
                    </span>
                </label>
            )}
            {invalid && <p role="status">素材不可读、时长未知或不满足镜头区间；不能采用。</p>}
            <Button disabled={disabled || invalid} onClick={() => void onAdopt(s.kind === "video" ? trim : 0)}>
                {s.kind === "video" ? "采用此视频" : "采用此旁白"}
            </Button>
        </div>
    );
}
