import { timelineTextOverflows } from "@/lib/timeline/timeline-text-overflow";
import type { TimelineOutput } from "@/types/timeline";
import { useState } from "react";
import type { TimelineClip, TimelineOverlay } from "@/types/timeline";

export function TimelineOverlayControls({ clip, output, onChange }: { clip: TimelineClip; output?: TimelineOutput; onChange: (clip: TimelineClip) => void }) {
    const [error, setError] = useState("");
    if (clip.kind !== "text" && clip.kind !== "image") return null;
    const overlay = clip.overlay ?? (clip.kind === "image" ? { x: 0.8, y: 0.05, width: 0.15, height: 0.1 } : { x: 0.1, y: 0.1, width: 0.8, height: 0.2 });
    const set = (patch: Partial<TimelineOverlay>) => {
        const next = { ...overlay, ...patch };
        if (next.x < 0 || next.y < 0 || next.width <= 0 || next.height <= 0 || next.x + next.width > 1 || next.y + next.height > 1 || (next.opacity ?? 1) < 0 || (next.opacity ?? 1) > 1 || (next.fontSize ?? 0.03) < 0.01 || (next.fontSize ?? 0.03) > 0.1) {
            setError("位置、尺寸或透明度超出范围，请先缩小框或调整位置。");
            return;
        }
        setError("");
        onChange({ ...clip, overlay: next });
    };
    const field = "rounded border border-[var(--border-default)] bg-transparent px-2 py-1 w-full";
    return (
        <fieldset className="grid grid-cols-2 gap-2 p-2 text-xs" data-canvas-no-zoom>
            {error && (
                <p className="col-span-2" role="alert">
                    {error}
                </p>
            )}
            <legend>广告叠加 · {clip.kind === "image" ? "Logo" : "卖点文字"}</legend>
            {clip.kind === "image" && !clip.overlay && (
                <button type="button" className={field} onClick={() => set({})}>
                    将图片改为 Logo 叠加
                </button>
            )}
            {clip.kind === "text" && (
                <label className="col-span-2">
                    文字内容
                    <textarea className={field} rows={2} value={clip.text ?? ""} onChange={(e) => onChange({ ...clip, text: e.target.value })} />
                </label>
            )}
            {([["x", "横向位置"], ["y", "纵向位置"], ["width", "框宽"], ["height", "框高"], ["opacity", "不透明度"], ...(clip.kind === "text" ? [["fontSize", "字号比例"]] : [])] as [keyof TimelineOverlay, string][]).map(([key, label]) => (
                <label key={key}>
                    {label}
                    <input
                        className={field}
                        type="number"
                        min={key === "fontSize" ? 0.01 : key === "width" || key === "height" ? 0.01 : 0}
                        max={key === "fontSize" ? 0.1 : 1}
                        step="0.01"
                        value={Number(overlay[key] ?? (key === "opacity" ? 1 : 0.03))}
                        onChange={(e) => {
                            if (Number.isFinite(e.target.valueAsNumber)) set({ [key]: e.target.valueAsNumber });
                        }}
                    />
                </label>
            ))}
            <label>
                开始（毫秒）
                <input
                    className={field}
                    type="number"
                    min="0"
                    step="100"
                    value={clip.startMs}
                    onChange={(e) => {
                        if (e.target.valueAsNumber >= 0) onChange({ ...clip, startMs: e.target.valueAsNumber });
                    }}
                />
            </label>
            <label>
                时长（毫秒）
                <input
                    className={field}
                    type="number"
                    min="100"
                    step="100"
                    value={clip.durationMs}
                    onChange={(e) => {
                        if (e.target.valueAsNumber > 0) onChange({ ...clip, durationMs: e.target.valueAsNumber });
                    }}
                />
            </label>
            {clip.kind === "text" && (
                <>
                    <label>
                        文字颜色
                        <input type="color" value={overlay.color ?? "#FFFFFF"} onChange={(e) => set({ color: e.target.value })} />
                    </label>
                    <label>
                        背景色
                        <input type="color" value={overlay.background ?? "#000000"} onChange={(e) => set({ background: e.target.value })} />
                    </label>
                    <button type="button" onClick={() => set({ background: undefined })}>
                        移除背景
                    </button>
                    <label>
                        对齐
                        <select className={field} value={overlay.align ?? "center"} onChange={(e) => set({ align: e.target.value as TimelineOverlay["align"] })}>
                            <option value="left">左</option>
                            <option value="center">中</option>
                            <option value="right">右</option>
                        </select>
                    </label>
                </>
            )}
            <p className="col-span-2 text-[var(--text-secondary)]">固定中文字体，最多两行、200字；文字过宽请缩小字号或手动换行。预览近似，最终以服务端成片为准。</p>
            {timelineTextOverflows(clip, output) && (
                <p className="col-span-2" role="alert">
                    文字超出框，请缩小字号、加大框或手动换行；导出前必须调整。
                </p>
            )}
            {(overlay.x + overlay.width > 1 || overlay.y + overlay.height > 1 || Array.from(clip.text ?? "").length > 200 || (clip.text ?? "").split("\n").length > 2) && (
                <p className="col-span-2" role="alert">
                    位置超框或文字过长，请调整后导出。
                </p>
            )}
        </fieldset>
    );
}
