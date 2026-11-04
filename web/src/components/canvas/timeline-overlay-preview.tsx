import type { TimelineClip, TimelineProject } from "@/types/timeline";

export function TimelineOverlayPreview({ timeline, timeMs, resolveImage }: { timeline: TimelineProject; timeMs: number; resolveImage: (clip: TimelineClip) => string | undefined }) {
    const tracks = new Map(timeline.tracks.map((t) => [t.id, t]));
    const layers = timeline.clips
        .filter(
            (c) =>
                tracks.get(c.trackId)?.visible !== false &&
                tracks.has(c.trackId) &&
                timeMs >= c.startMs &&
                timeMs < c.startMs + c.durationMs &&
                (c.kind === "text" || (c.kind === "image" && c.overlay) || (c.kind === "subtitle" && timeline.output?.burnSubtitles)),
        )
        .sort((a, b) => {
            const rank = (c: TimelineClip) => (c.kind === "image" ? 0 : c.kind === "text" ? 1 : 2);
            return rank(a) - rank(b) || (tracks.get(a.trackId)?.order ?? 0) - (tracks.get(b.trackId)?.order ?? 0) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
        });
    return (
        <div className="pointer-events-none absolute inset-0 overflow-hidden" style={{ containerType: "size" }} aria-label="广告叠加预览">
            {layers.map((c) => {
                const sub = c.kind === "subtitle";
                const o = sub ? { x: 0.05, y: 0, width: 0.9, height: 1, fontSize: 0.03 } : (c.overlay ?? { x: 0.1, y: 0.1, width: 0.8, height: 0.2 });
                const style = {
                    position: "absolute" as const,
                    left: `${o.x * 100}%`,
                    top: sub ? undefined : `${o.y * 100}%`,
                    bottom: sub ? "6%" : undefined,
                    width: `${o.width * 100}%`,
                    height: sub ? undefined : `${o.height * 100}%`,
                    opacity: o.opacity ?? 1,
                    color: o.color ?? "#FFFFFF",
                    background: o.background,
                    fontSize: `${(o.fontSize ?? 0.03) * 100}cqh`,
                    fontFamily: "Noto Sans CJK SC, sans-serif",
                    textAlign: o.align ?? ("center" as const),
                    whiteSpace: "pre" as const,
                    lineHeight: 1.2,
                };
                if (c.kind === "image") {
                    const src = resolveImage(c);
                    return src ? <img key={c.id} src={src} alt={c.title || "Logo"} style={{ ...style, objectFit: "contain" }} /> : null;
                }
                return (
                    <div key={c.id} style={style}>
                        {c.text}
                    </div>
                );
            })}
        </div>
    );
}
