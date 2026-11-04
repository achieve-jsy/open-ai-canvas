import type { TimelineClip, TimelineDirectMedia, TimelineProject } from "@/types/timeline";
import { normalizeTimelineProject } from "./timeline-tracks";

/** Freeze uploaded resources for the worker; never silently drop a missing source. */
export function resolveTimelineRenderSnapshot(project: TimelineProject, resolve: (clip: TimelineClip) => TimelineDirectMedia | undefined): TimelineProject {
    const snapshot = structuredClone(normalizeTimelineProject(project));
    const tracks = new Map(snapshot.tracks.map((track) => [track.id, track]));
    for (const clip of snapshot.clips) {
        const track = tracks.get(clip.trackId);
        if (!track) throw new Error("片段引用了不存在的轨道");
        if (track.visible === false || !["video", "audio", "image"].includes(clip.kind)) continue;
        const media = clip.directMedia ?? resolve(clip);
        if (!media?.storageKey?.match(/^resource:[^\s]+$/) || media.kind !== clip.kind) {
            throw new Error(`请先上传片段「${clip.title || clip.id}」的源素材`);
        }
        // The worker needs a resource reference, not browser-only URLs or large data URLs.
        clip.directMedia = { id: media.id, kind: media.kind, title: media.title, storageKey: media.storageKey };
    }
    return snapshot;
}
