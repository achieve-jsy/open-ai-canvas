import type { ReactNode, Ref } from "react";

export type AdvertisingStage = "brief" | "script" | "shots" | "preview" | "export";
export const advertisingStages: { id: AdvertisingStage; label: string; title: string; description: string }[] = [
    { id: "brief", label: "素材与需求", title: "先准备，再创作", description: "一张商品图，一段场景描述。其他复杂的事，逐步来。" },
    { id: "script", label: "脚本确认", title: "决定这支广告怎么拍", description: "先核对内容，再授权视频生成。脚本示意不是生成视频。" },
    { id: "shots", label: "镜头制作", title: "生成只是开始，选用才算完成", description: "逐镜头查看报价、授权、检查与采用。新候选不会自动替换已有选择。" },
    { id: "preview", label: "成片预览", title: "把镜头连成一支广告", description: "检查镜头顺序、裁切与声音。时间线示意不等于完整 MP4 混音。" },
    { id: "export", label: "导出交付", title: "检查最终文件，再交付", description: "复用此组装的真实渲染结果；下载不代表广告已通过人工验收。" },
];

// Page-private shell: semantic surface/control tokens adapt to the product's light/dark theme.
export function AdvertisingWorkbench({
    stage,
    onStageChange,
    facts,
    summary,
    children,
    primary,
    actionRef,
}: {
    stage: AdvertisingStage;
    onStageChange: (stage: AdvertisingStage) => void;
    facts: Record<AdvertisingStage, string>;
    summary: ReactNode;
    children: ReactNode;
    primary?: ReactNode;
    actionRef: Ref<HTMLDivElement>;
}) {
    const index = advertisingStages.findIndex((s) => s.id === stage);
    const current = advertisingStages[index];
    return (
        <div className="ad-workbench">
            <nav className="ad-stages" aria-label="广告制作阶段">
                {advertisingStages.map((s, i) => (
                    <button key={s.id} type="button" aria-label={s.label} aria-current={stage === s.id ? "step" : undefined} onClick={() => onStageChange(s.id)}>
                        <span className="ad-stage-number" aria-hidden="true">
                            {i + 1}
                        </span>
                        <span>
                            {s.label}
                            <small>{facts[s.id]}</small>
                        </span>
                    </button>
                ))}
            </nav>
            <div className="ad-workbench-grid">
                <div className="ad-stage-content">
                    <header className="ad-stage-heading">
                        <p className="ad-eyebrow">AD CAMPAIGN / 0{index + 1}</p>
                        <h2>{current.title}</h2>
                        <p>{current.description}</p>
                    </header>
                    {children}
                </div>
                <aside className="ad-summary" aria-label="项目概览和保存状态">
                    {summary}
                </aside>
            </div>
            <footer className="ad-action-bar">
                <p role="status">
                    {facts[stage]}
                    <small>保存不是批准；视频生成仍需单独确认费用。</small>
                </p>
                <div className="ad-actions">
                    <button type="button" disabled={index === 0} onClick={() => onStageChange(advertisingStages[index - 1].id)}>
                        上一步
                    </button>
                    <div ref={actionRef}>{primary}</div>
                </div>
            </footer>
        </div>
    );
}
