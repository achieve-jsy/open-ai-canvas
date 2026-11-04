// 默认由后端 ffmpeg 渲染并把产物写入资源存储；浏览器 ffmpeg.wasm 只承担离线降级。
// 服务端快照拒绝缺失引用，增强时间线不静默降级。

import { resolveTimelineRenderSnapshot } from "@/lib/timeline/timeline-render-snapshot";
import { TimelineOverlayControls } from "@/components/canvas/timeline-overlay-controls";
import { useMemo, useState } from "react";
import { Download, Loader2, PackageOpen, Server } from "lucide-react";

import { useEditorStoreContext } from "@/components/editor/editor-context";
import { buildTimelineRenderPlan, type TimelineRenderSource } from "@/lib/timeline/timeline-to-ffmpeg";
import { exportTimelineToMp4, type TimelineExportProgress } from "@/lib/timeline/timeline-export";
import { resourceFileUrl } from "@/services/api/resources";
import { waitForGenerationTask } from "@/services/api/task-center";
import { createTimelineRenderTask, type TimelineRenderResult } from "@/services/api/timeline-tasks";
import type { TimelineProject } from "@/types/timeline";

type ExportState = {
    phase: "idle" | "running" | "done" | "error";
    mode: "remote" | "local" | null;
    percent: number;
    detail: string;
    result: TimelineRenderResult | null;
};

/** 从时间线 clip 收集渲染源（按 nodeId 关联；directMedia 提供本地媒体定位）。 */
function collectRenderSources(project: TimelineProject): TimelineRenderSource[] {
    const seen = new Set<string>();
    const sources: TimelineRenderSource[] = [];
    for (const clip of project.clips) {
        if (clip.kind !== "video" && clip.kind !== "image") continue;
        const direct = clip.directMedia;
        if (!direct) continue;
        if (seen.has(clip.nodeId)) continue;
        seen.add(clip.nodeId);
        sources.push({
            nodeId: clip.nodeId,
            fileName: `input-${sources.length}.mp4`,
            durationMs: clip.durationMs,
            storageKey: direct.storageKey,
            url: direct.url,
        });
    }
    return sources;
}

export function EditorExport() {
    const { project, dispatch, selectedClipId, selectClip, transportMs } = useEditorStoreContext();
    const [taskId, setTaskId] = useState("");
    const selected = project?.clips.find((c) => c.id === selectedClipId);
    // Standalone export relies on user-owned resources; no inferred Project ID.
    const [state, setState] = useState<ExportState>({ phase: "idle", mode: null, percent: 0, detail: "", result: null });

    const sources = useMemo(() => (project ? collectRenderSources(project) : []), [project]);
    const enhanced = Boolean(project?.output || project?.clips.some((c) => c.overlay || c.kind === "text"));
    const plan = useMemo(() => (project && !enhanced ? buildTimelineRenderPlan(project, sources) : null), [project, sources, enhanced]);

    // 主路径：提交后端渲染任务并轮询（服务端任务上限 60 分钟，前端多留余量）。
    const renderRemote = async () => {
        if (!project || sources.length === 0 || state.phase === "running") return;
        setState({ phase: "running", mode: "remote", percent: 0, detail: "提交渲染任务…", result: null });
        try {
            const snapshot = resolveTimelineRenderSnapshot(project, () => undefined);
            const created = await createTimelineRenderTask({ timeline: snapshot });
            setTaskId(created.id);
            const done = await waitForGenerationTask(created.id, {
                timeoutMs: 62 * 60 * 1000,
                intervalMs: 3000,
                onTaskUpdate: (task) =>
                    setState({
                        phase: "running",
                        mode: "remote",
                        percent: task.progress ?? 0,
                        detail: task.stage ? `${task.stage}${task.progress ? ` · ${task.progress}%` : ""}` : "渲染中…",
                        result: null,
                    }),
            });
            const parsed = JSON.parse(done.resultJson ?? "{}") as TimelineRenderResult;
            if (!parsed.resourceId) throw new Error("渲染任务未返回产物");
            if (snapshot.output?.burnSubtitles && snapshot.clips.some((c) => c.kind === "subtitle" && c.text?.trim() && snapshot.tracks.find((t) => t.id === c.trackId)?.visible !== false) && !parsed.subtitlesBurned)
                throw new Error("服务端未烧录字幕，请检查任务结果");
            setState({
                phase: "done",
                mode: "remote",
                percent: 100,
                detail: `渲染完成：${parsed.fileName ?? "timeline.mp4"}`,
                result: parsed,
            });
        } catch (error) {
            setState({
                phase: "error",
                mode: "remote",
                percent: 0,
                detail: error instanceof Error ? error.message : "渲染失败",
                result: null,
            });
        }
    };

    // 降级路径：ffmpeg.wasm 浏览器本地合成（无后端/离线时可用）。
    const exportLocalMp4 = async () => {
        if (!project || sources.length === 0 || state.phase === "running") return;
        setState({ phase: "running", mode: "local", percent: 0, detail: "准备导出", result: null });
        try {
            const { blob, warnings } = await exportTimelineToMp4(project, sources, {
                onProgress: (p: TimelineExportProgress) => setState({ phase: "running", mode: "local", percent: p.percent, detail: p.detail, result: null }),
            });
            const url = URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = url;
            a.download = "timeline-export.mp4";
            a.click();
            URL.revokeObjectURL(url);
            setState({ phase: "done", mode: "local", percent: 100, detail: warnings.length ? `导出完成，已开始下载；${warnings.join("；")}` : "导出完成，已开始下载", result: null });
        } catch (error) {
            setState({
                phase: "error",
                mode: "local",
                percent: 0,
                detail: error instanceof Error ? error.message : "导出失败",
                result: null,
            });
        }
    };

    if (!project) return null;

    return (
        <div className="flex h-full flex-col bg-[var(--previs-sequencer-surface)]">
            <div className="director-scroll min-h-0 flex-1 overflow-y-auto p-3">
                <button type="button" className="mb-2 rounded border p-2 text-xs" onClick={() => dispatch({ op: "setTimelineOutput", payload: { width: 1080, height: 1920, fit: "pad", burnSubtitles: true } })}>
                    广告竖屏 · 9:16 · 硬字幕
                </button>
                <button
                    type="button"
                    className="mb-2 ml-2 rounded border p-2 text-xs"
                    onClick={() => {
                        if (!project) return;
                        let track = project.tracks.find((t) => t.kind === "text");
                        if (!track) {
                            dispatch({ op: "addTrack", payload: { kind: "text" } });
                            track = { id: "text-1", kind: "text", label: "文本", order: 0 };
                        }
                        const id = crypto.randomUUID();
                        dispatch({ op: "addClip", payload: { clip: { id, nodeId: id, kind: "text", trackId: track.id, startMs: transportMs, durationMs: Math.max(100, Math.min(3000, project.durationMs - transportMs)), text: "立即购买" } } });
                        selectClip(id);
                    }}
                >
                    添加卖点文字
                </button>
                {selected && (
                    <TimelineOverlayControls
                        clip={selected}
                        onChange={(clip) => {
                            dispatch({ op: "setClipProperty", payload: { id: clip.id, patch: { text: clip.text, overlay: clip.overlay } } });
                            if (clip.startMs !== selected.startMs || clip.durationMs !== selected.durationMs) dispatch({ op: "trimClip", payload: { id: clip.id, startMs: clip.startMs, durationMs: clip.durationMs } });
                        }}
                    />
                )}
                {taskId && (
                    <p className="break-all text-xs">
                        任务 {taskId} ·{" "}
                        <a href="/tasks" target="_blank" rel="noreferrer">
                            任务中心
                        </a>
                        ；等待中断请先查询，勿重复提交。
                    </p>
                )}
                <div className="mb-3 rounded-md border border-[var(--previs-sequencer-border)] bg-[var(--previs-control-hover)] p-2">
                    <div className="flex items-center justify-between">
                        <span className="text-xs font-medium text-[var(--previs-dock-fg-strong)]">渲染计划</span>
                        <span className="text-[var(--fs-label)] text-[var(--previs-dock-fg)]/60">{plan ? `${plan.steps.length} 步` : "—"}</span>
                    </div>
                    {plan && plan.steps.length > 0 ? (
                        <ul className="mt-2 space-y-1">
                            {plan.steps.slice(0, 8).map((step, index) => (
                                <li key={index} className="flex items-center gap-2 text-[var(--fs-label)] text-[var(--previs-dock-fg)]/70">
                                    <span className="shrink-0 rounded bg-[var(--previs-dock-active-surface)] px-1 py-0.5 font-mono text-[var(--fs-label)] uppercase text-[var(--previs-dock-fg)]/80">{step.kind}</span>
                                    <span className="truncate">{step.description}</span>
                                </li>
                            ))}
                            {plan.steps.length > 8 && <li className="pt-0.5 text-[var(--fs-label)] text-[var(--previs-dock-fg)]/50">…共 {plan.steps.length} 步</li>}
                        </ul>
                    ) : (
                        <p className="mt-2 text-[var(--fs-label)] text-[var(--previs-dock-fg)]/55">{enhanced ? "增强时间线将由服务端合成；普通本地导出不可用。" : "时间线没有可渲染的视频片段。"}</p>
                    )}
                </div>

                <div className="mb-3 flex items-center justify-between rounded-md border border-[var(--previs-sequencer-border)] bg-[var(--previs-control-hover)] px-2 py-2 text-[var(--fs-label)] text-[var(--previs-dock-fg)]/70">
                    <span className="flex items-center gap-2">
                        <PackageOpen className="size-3.5 text-[var(--previs-dock-fg)]/60" />
                        渲染源
                    </span>
                    <span className="tabular-nums text-[var(--previs-dock-fg)]/80">{sources.length} 个</span>
                </div>

                <button
                    type="button"
                    onClick={renderRemote}
                    disabled={sources.length === 0 || state.phase === "running"}
                    className="flex w-full items-center justify-center gap-2 rounded-md bg-[var(--previs-accent)] px-2 py-2 text-xs font-medium text-[var(--previs-on-accent)] hover:bg-[var(--previs-accent-hover)] disabled:opacity-40"
                >
                    {state.phase === "running" && state.mode === "remote" ? <Loader2 className="size-3.5 animate-spin" /> : <Server className="size-3.5" />}
                    {state.phase === "running" && state.mode === "remote" ? "渲染中…" : "渲染成片（服务端）"}
                </button>

                <button
                    type="button"
                    onClick={exportLocalMp4}
                    disabled={sources.length === 0 || state.phase === "running"}
                    className="mt-2 flex w-full items-center justify-center gap-2 rounded-md border border-[var(--previs-sequencer-border)] px-2 py-2 text-xs text-[var(--previs-dock-fg)]/80 hover:bg-[var(--previs-control-hover)] disabled:opacity-40"
                >
                    {state.phase === "running" && state.mode === "local" ? <Loader2 className="size-3.5 animate-spin" /> : <Download className="size-3.5" />}
                    {state.phase === "running" && state.mode === "local" ? "导出中…" : "本地导出（ffmpeg.wasm）"}
                </button>

                {state.phase === "running" && (
                    <div className="mt-3">
                        <div className="mb-1 flex justify-between text-[var(--fs-label)] text-[var(--previs-dock-fg)]/70">
                            <span>{state.detail}</span>
                            <span className="tabular-nums">{state.percent}%</span>
                        </div>
                        <div className="h-1.5 overflow-hidden rounded-full bg-[var(--previs-control-hover)]">
                            <div className="h-full rounded-full bg-[var(--previs-accent)] transition-all" style={{ width: `${state.percent}%` }} />
                        </div>
                    </div>
                )}

                {state.phase === "done" && state.result && state.mode === "remote" && (
                    <div className="mt-2 overflow-hidden rounded-md border border-[var(--previs-sequencer-border)] bg-[var(--previs-control-hover)]">
                        <video src={resourceFileUrl(state.result.resourceId)} controls className="aspect-video w-full bg-black" />
                        <div className="flex items-center justify-between gap-2 px-2 py-2 text-[var(--fs-label)] text-[var(--previs-dock-fg)]/80">
                            <span className="truncate">{state.result.fileName ?? "timeline.mp4"}</span>
                            <a href={resourceFileUrl(state.result.resourceId)} download={state.result.fileName ?? "timeline.mp4"} className="flex shrink-0 items-center gap-1 text-[var(--previs-accent)] hover:underline">
                                <Download className="size-3" />
                                下载
                            </a>
                        </div>
                    </div>
                )}
                {state.phase === "done" && (!state.result || state.mode !== "remote") && <p className="mt-2 text-[var(--fs-label)] text-[var(--previs-success)]">{state.detail}</p>}
                {state.phase === "error" && <p className="mt-2 text-[var(--fs-label)] text-[var(--previs-danger)]">{state.detail}</p>}

                <p className="mt-3 text-[var(--fs-label)] leading-relaxed text-[var(--previs-dock-fg)]/55">默认提交服务端渲染任务（异步，产物可直接预览/下载）；本地 ffmpeg.wasm 导出保留为离线兜底。</p>
                {sources.length === 0 && <p className="mt-1 text-[var(--fs-label)] text-[var(--previs-dock-fg)]/55">请添加并上传可用媒体；缺失引用将阻止服务端导出。</p>}
            </div>
        </div>
    );
}
