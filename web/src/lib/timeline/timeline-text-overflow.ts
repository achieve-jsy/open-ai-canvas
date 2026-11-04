import type { TimelineClip, TimelineOutput, TimelineProject } from "@/types/timeline";

function measureText(text: string, size: number): number {
    if (typeof document === "undefined") return NaN;
    const context = document.createElement("canvas").getContext("2d");
    if (!context) return NaN;
    context.font = size + 'px "Noto Sans CJK SC", sans-serif';
    return context.measureText(text).width;
}

export function timelineTextOverflows(clip: TimelineClip, output?: TimelineOutput, measure = measureText): boolean | undefined {
    if (clip.kind !== "text" || !clip.text) return false;
    const o = clip.overlay ?? { x: 0.1, y: 0.1, width: 0.8, height: 0.2 };
    const width = output?.width ?? 1920,
        height = output?.height ?? 1080;
    const size = (o.fontSize ?? 0.03) * height;
    const lines = clip.text.split("\n");
    const widths = lines.map((line) => measure(line, size));
    if (widths.some((w) => !Number.isFinite(w))) return undefined;
    return widths.some((w) => w > o.width * width) || lines.length * size * 1.2 > o.height * height;
}

export async function assertTimelineTextFits(project: TimelineProject): Promise<void> {
    const clips = project.clips.filter((c) => c.kind === "text" && c.text && project.tracks.find((t) => t.id === c.trackId)?.visible !== false);
    if (!clips.length) return;
    await document.fonts?.ready;
    for (const clip of clips) {
        const overflow = timelineTextOverflows(clip, project.output);
        if (overflow === undefined) throw new Error("无法测量文字，请使用支持Canvas的浏览器导出");
        if (overflow) throw new Error("文字「" + (clip.title || clip.id) + "」超出框，请缩小字号、加大框或手动换行");
    }
}
