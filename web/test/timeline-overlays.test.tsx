import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";
import { TimelineOverlayPreview } from "../src/components/canvas/timeline-overlay-preview";
import { TimelineOverlayControls } from "../src/components/canvas/timeline-overlay-controls";
import { assertBrowserTimelineSupported } from "../src/lib/timeline/timeline-render-snapshot";
import type { TimelineProject } from "../src/types/timeline";
import { createEditorCommandRegistry } from "../src/lib/timeline/editor-commands";
import { assertTimelineTextFits, timelineTextOverflows } from "../src/lib/timeline/timeline-text-overflow";
import { getPreviewClipAtTime } from "../src/lib/timeline/timeline-clip-view";

const timeline: TimelineProject = {
    version: 2,
    durationMs: 3000,
    output: { width: 320, height: 568, fit: "pad", burnSubtitles: true },
    tracks: [{ id: "text", kind: "text", label: "text", order: 0 }],
    clips: [{ id: "cta", nodeId: "cta", kind: "text", trackId: "text", startMs: 1000, durationMs: 1000, text: "<b>购买</b>" }],
};
test("editor persists output and overlays without selecting Logo as main frame", () => {
    const r = createEditorCommandRegistry();
    const o = { x: 0.1, y: 0.1, width: 0.8, height: 0.2 };
    const next = r.apply(timeline, { op: "setClipProperty", payload: { id: "cta", patch: { overlay: o } } });
    expect(next.clips[0].overlay).toEqual(o);
    expect(() => r.apply(timeline, { op: "setClipProperty", payload: { id: "cta", patch: { overlay: { ...o, x: NaN } } } })).toThrow();
    expect(() => r.apply(timeline, { op: "addClip", payload: { clip: { ...timeline.clips[0], id: "bad", overlay: { ...o, width: -1 } } } })).toThrow();
    expect(r.apply(timeline, { op: "setTimelineOutput", payload: timeline.output }).output).toEqual(timeline.output);
    expect(() => r.apply(timeline, { op: "setTimelineOutput", payload: { width: 1 } })).toThrow();
    expect(getPreviewClipAtTime({ ...timeline, clips: [{ ...timeline.clips[0], kind: "image", overlay: o }] }, 1500, "cta")).toBeNull();
});

test("text overflow uses measured lines and output aspect", () => {
    const clip = { ...timeline.clips[0], text: "中".repeat(20), overlay: { x: 0.1, y: 0.1, width: 0.8, height: 0.2, fontSize: 0.1 } };
    const measure = (text: string, fontSize: number) => Array.from(text).length * fontSize;
    expect(timelineTextOverflows(clip, { width: 1080, height: 1920, fit: "pad", burnSubtitles: true }, measure)).toBe(true);
    expect(timelineTextOverflows({ ...clip, text: "短句", overlay: { ...clip.overlay, fontSize: 0.03 } }, timeline.output, measure)).toBe(false);
    expect(timelineTextOverflows({ ...clip, text: "中\n中", overlay: { ...clip.overlay, height: 0.1 } }, timeline.output, measure)).toBe(true);
});

test("both server exports invoke shared measured text guard", () => {
    for (const file of ["../src/components/canvas/canvas-timeline-dialog.tsx", "../src/lib/plugins/builtin/editor/editor-export.tsx"]) {
        const source = readFileSync(new URL(file, import.meta.url), "utf8");
        expect(source.includes("await assertTimelineTextFits(snapshot)")).toBe(true);
    }
    expect(typeof assertTimelineTextFits).toBe("function");
});

test("ordinary preset changes do not opt into enhanced export", () => {
    const source = readFileSync(new URL("../src/components/canvas/canvas-timeline-dialog.tsx", import.meta.url), "utf8");
    expect(source.includes("if (draft.output) applyDraft")).toBe(true);
});

test("overlay interval and literal text", () => {
    const view = (timeMs: number) => renderToStaticMarkup(<TimelineOverlayPreview timeline={timeline} timeMs={timeMs} resolveImage={() => undefined} />);
    expect(view(999)).not.toContain("购买");
    expect(view(1000)).toContain("&lt;b&gt;购买&lt;/b&gt;");
    expect(view(2000)).not.toContain("购买");
    const hidden = { ...timeline, tracks: [{ ...timeline.tracks[0], visible: false }] };
    expect(renderToStaticMarkup(<TimelineOverlayPreview timeline={hidden} timeMs={1500} resolveImage={() => undefined} />)).not.toContain("购买");
});
test("overlay controls expose labelled native keyboard inputs", () => {
    const html = renderToStaticMarkup(<TimelineOverlayControls clip={timeline.clips[0]} onChange={() => {}} />);
    expect(html).toContain("文字内容");
    expect(html).toContain("横向位置");
    expect(html).toContain('type="number"');
});
test("browser rejects enhanced output before loading wasm", () => {
    expect(() => assertBrowserTimelineSupported(timeline)).toThrow("服务端");
    expect(() => assertBrowserTimelineSupported({ ...timeline, clips: [], output: { ...timeline.output!, burnSubtitles: false } })).toThrow("服务端");
    expect(() => assertBrowserTimelineSupported({ ...timeline, output: undefined, clips: [] })).not.toThrow();
});
