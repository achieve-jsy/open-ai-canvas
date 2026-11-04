import { expect, test } from "bun:test";
import { normalizeTimelineProject } from "../src/lib/timeline/timeline-tracks";
import type { TimelineProject } from "../src/types/timeline";

const output = { width: 1080, height: 1920, fit: "pad" as const, burnSubtitles: true };
test("normalization preserves output and first default track visibility, mute and order", () => {
    const result = normalizeTimelineProject({
        version: 2,
        output,
        tracks: [
            { id: "video-1", kind: "video", label: "hidden", order: 9, visible: false, muted: true },
            { id: "video-1", kind: "video", label: "duplicate", order: 0 },
        ],
        clips: [],
        durationMs: 0,
    });
    expect(result.output).toEqual(output);
    expect(result.tracks.find((t) => t.id === "video-1")).toMatchObject({ label: "hidden", visible: false, muted: true, order: 9 });
});

test("snapshot resolves all visible media, keeps explicit references and is independent", async () => {
    const { resolveTimelineRenderSnapshot } = await import("../src/lib/timeline/timeline-render-snapshot");
    const project: TimelineProject = {
        version: 2,
        output,
        tracks: [
            { id: "image", kind: "image", label: "Logo", order: 3 },
            { id: "hidden", kind: "audio", label: "hidden", order: 4, visible: false },
        ],
        durationMs: 3000,
        clips: [
            { id: "v", nodeId: "v", trackId: "video-1", kind: "video", startMs: 0, durationMs: 3000, directMedia: { id: "v", kind: "video", title: "original", storageKey: "resource:original" } },
            { id: "a", nodeId: "a", trackId: "audio-1", kind: "audio", startMs: 0, durationMs: 3000 },
            { id: "logo", nodeId: "logo", trackId: "image", kind: "image", startMs: 1000, durationMs: 1000, overlay: { x: 0.8, y: 0.05, width: 0.15, height: 0.1 } },
            { id: "hidden", nodeId: "h", trackId: "hidden", kind: "audio", startMs: 0, durationMs: 3000 },
        ],
    };
    const snapshot = resolveTimelineRenderSnapshot(project, (clip) => ({ id: clip.id, kind: clip.kind as "audio", title: clip.id, storageKey: "resource:" + clip.id }));
    expect(snapshot.clips.map((c) => c.directMedia?.storageKey)).toEqual(["resource:original", "resource:a", "resource:logo", undefined]);
    snapshot.output!.width = 320;
    snapshot.clips[0].directMedia!.title = "changed";
    snapshot.clips[2].overlay!.x = 0;
    expect(project.output!.width).toBe(1080);
    expect(project.clips[0].directMedia!.title).toBe("original");
    expect(normalizeTimelineProject(project).clips[2].overlay?.x).toBe(0.8);
});

test("snapshot rejects missing or URL-only sources rather than silently dropping them", async () => {
    const { resolveTimelineRenderSnapshot } = await import("../src/lib/timeline/timeline-render-snapshot");
    const project: TimelineProject = { version: 2, tracks: [], durationMs: 1000, clips: [{ id: "a", nodeId: "a", trackId: "audio-1", kind: "audio", startMs: 0, durationMs: 1000 }] };
    expect(() => resolveTimelineRenderSnapshot(project, () => undefined)).toThrow("上传");
    expect(() => resolveTimelineRenderSnapshot(project, () => ({ id: "a", kind: "audio", title: "a", url: "https://example.com/a.mp3" }))).toThrow("上传");
});
