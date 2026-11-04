import { expect, test } from "bun:test";
import { Window } from "happy-dom";
import { spawnSync } from "node:child_process";
import type { TimelineClip } from "../src/types/timeline";

test("native overlay controls edit text, placement and Logo without losing saved fields", async () => {
    // React caches DOM capability at import time; isolate from SSR tests in the full suite.
    if (!process.env.CANVAS_OVERLAY_DOM_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path], { env: { ...process.env, CANVAS_OVERLAY_DOM_CHECK: "1" }, encoding: "utf8", timeout: 20000 });
        expect(child.stderr + child.stdout).not.toContain("(fail)");
        expect(child.status).toBe(0);
        return;
    }
    const dom = new Window({ url: "http://localhost" });
    const originals = new Map<string, PropertyDescriptor | undefined>();
    for (const [key, value] of Object.entries({ window: dom, document: dom.document, navigator: dom.navigator, HTMLElement: dom.HTMLElement, Node: dom.Node, IS_REACT_ACT_ENVIRONMENT: true })) {
        originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
        Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
    }
    const { act } = await import("react");
    const { createRoot } = await import("react-dom/client");
    const { TimelineOverlayControls } = await import("../src/components/canvas/timeline-overlay-controls");
    const host = dom.document.createElement("div");
    dom.document.body.append(host);
    const root = createRoot(host as unknown as HTMLElement);
    let clip: TimelineClip = { id: "t", nodeId: "t", kind: "text", trackId: "text", startMs: 0, durationMs: 3000, text: "原文", overlay: { x: 0.1, y: 0.1, width: 0.8, height: 0.2 } };
    const render = () =>
        root.render(
            <TimelineOverlayControls
                clip={clip}
                onChange={(c) => {
                    clip = c;
                    render();
                }}
            />,
        );
    try {
        await act(render);
        const input = host.querySelector('input[type="number"]')!;
        await act(() => {
            Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, "value")!.set!.call(input, "0.15");
            input.dispatchEvent(new dom.Event("input", { bubbles: true }));
        });
        expect(clip.overlay?.x).toBe(0.15);
        await act(() => {
            Object.getOwnPropertyDescriptor(dom.HTMLInputElement.prototype, "value")!.set!.call(input, "0.9");
            input.dispatchEvent(new dom.Event("input", { bubbles: true }));
        });
        expect(host.querySelector("[role=alert]")).not.toBeNull();
        expect(clip.overlay?.x).toBe(0.15);
        const text = host.querySelector("textarea")!;
        await act(() => {
            Object.getOwnPropertyDescriptor(dom.HTMLTextAreaElement.prototype, "value")!.set!.call(text, "购买\n立即行动");
            text.dispatchEvent(new dom.Event("input", { bubbles: true }));
        });
        expect(clip.text).toBe("购买\n立即行动");
        expect(clip.overlay?.x).toBe(0.15);
        clip = { ...clip, kind: "image", overlay: undefined };
        await act(render);
        await act(() => host.querySelector("button")!.click());
        expect(clip.overlay).toEqual({ x: 0.8, y: 0.05, width: 0.15, height: 0.1 });
        const { CanvasTimelinePreview } = await import("../src/components/canvas/canvas-timeline-preview");
        const { canvasThemes } = await import("../src/lib/canvas-theme");
        let time = 0;
        let stopped = false;
        const timeline = { version: 2 as const, durationMs: 120, tracks: [{ id: "image", kind: "image" as const, label: "image", order: 0 }], clips: [{ ...clip, id: "still", trackId: "image", startMs: 0, durationMs: 60, overlay: undefined }] };
        const preview = () =>
            root.render(
                <CanvasTimelinePreview
                    timeline={timeline}
                    clips={timeline.clips}
                    nodes={[]}
                    playheadMs={time}
                    playing={!stopped}
                    theme={canvasThemes.dark}
                    onTogglePlay={() => {
                        stopped = true;
                        preview();
                    }}
                    onPlayheadChange={(ms) => {
                        time = ms;
                        preview();
                    }}
                />,
            );
        await act(preview);
        await act(() => new Promise((resolve) => setTimeout(resolve, 180)));
        expect(time).toBe(120);
        expect(stopped).toBe(true);
    } finally {
        await act(() => root.unmount());
        for (const [key, d] of originals) {
            if (d) Object.defineProperty(globalThis, key, d);
            else Reflect.deleteProperty(globalThis, key);
        }
        await dom.happyDOM.close();
    }
});
