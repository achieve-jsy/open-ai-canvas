import type { AdvertisingDraft, AdvertisingShot } from "@/services/api/advertising-project";

// Native labelled fields inherit workspace tokens; this form owns no server state.
export function AdvertisingDraftForm({ value, onChange, disabled = false }: { value: AdvertisingDraft; onChange: (next: AdvertisingDraft) => void; disabled?: boolean }) {
    const brief = (patch: Partial<AdvertisingDraft["brief"]>) => onChange({ ...value, brief: { ...value.brief, ...patch } });
    const brand = (patch: Partial<AdvertisingDraft["brand"]>) => onChange({ ...value, brand: { ...value.brand, ...patch } });
    const shot = (id: string, patch: Partial<AdvertisingShot>) => onChange({ ...value, shots: value.shots.map((s) => (s.id === id ? { ...s, ...patch } : s)) });
    const move = (index: number, delta: number) => {
        const shots = [...value.shots];
        [shots[index], shots[index + delta]] = [shots[index + delta], shots[index]];
        onChange({ ...value, shots });
    };
    const pool = [...new Set([...value.productResourceIds, ...value.additionalResourceIds])];
    return (
        <fieldset disabled={disabled} className="ad-form space-y-5 min-w-0">
            <section className="ad-section">
                <h2>需求与品牌</h2>
                <p className="text-muted-foreground">中文 · 单商品 · 竖屏 1080×1920 · 15–30秒。草稿可不完整，确认时须满足全部要求。</p>
                <div className="grid gap-4 sm:grid-cols-2">
                    <label>
                        商品名称
                        <input aria-label="商品名称" maxLength={120} value={value.brief.productName} onChange={(e) => brief({ productName: e.target.value })} />
                    </label>
                    <label>
                        目标受众
                        <input maxLength={300} value={value.brief.audience} onChange={(e) => brief({ audience: e.target.value })} />
                    </label>
                    <label className="sm:col-span-2">
                        商品描述
                        <textarea rows={3} maxLength={2000} value={value.brief.description} onChange={(e) => brief({ description: e.target.value })} />
                    </label>
                    <div>
                        <span>核心卖点（最多5条）</span>
                        {value.brief.sellingPoints.map((point, i) => (
                            <label key={i}>
                                卖点{i + 1}
                                <div className="flex gap-2">
                                    <input aria-label={"卖点" + (i + 1)} maxLength={120} value={point} onChange={(e) => brief({ sellingPoints: value.brief.sellingPoints.map((p, j) => (i === j ? e.target.value : p)) })} />
                                    <button type="button" onClick={() => brief({ sellingPoints: value.brief.sellingPoints.filter((_, j) => i !== j) })}>
                                        移除
                                    </button>
                                </div>
                            </label>
                        ))}
                        <button type="button" disabled={value.brief.sellingPoints.length >= 5} onClick={() => brief({ sellingPoints: [...value.brief.sellingPoints, ""] })}>
                            添加卖点
                        </button>
                    </div>
                    <label>
                        行动号召（CTA）
                        <input maxLength={80} value={value.brief.cta} onChange={(e) => brief({ cta: e.target.value })} />
                    </label>
                    <label>
                        目标时长（秒）
                        <input type="number" min={15} max={30} step={0.001} value={value.brief.durationMs / 1000} onChange={(e) => brief({ durationMs: Math.round(Number(e.target.value) * 1000) })} />
                    </label>
                    <label>
                        品牌名称
                        <input maxLength={120} value={value.brand.name} onChange={(e) => brand({ name: e.target.value })} />
                    </label>
                    <label>
                        品牌色（选填 #RRGGBB）
                        <input pattern="#[0-9a-fA-F]{6}" placeholder="#123456" value={value.brand.primaryColor} onChange={(e) => brand({ primaryColor: e.target.value })} />
                    </label>
                </div>
            </section>
            <section className="ad-section">
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <h2>分镜脚本</h2>
                    <span>
                        合计 {value.shots.reduce((sum, s) => sum + s.durationMs, 0) / 1000} / {value.brief.durationMs / 1000} 秒
                    </span>
                </div>
                <p className="text-muted-foreground">确认需要3–5个镜头，时长总和须与目标一致。这里只编写脚本，不会生成媒体。</p>
                {value.shots.map((s, i) => (
                    <article key={s.id} className="ad-shot">
                        <div className="flex flex-wrap gap-2 items-center">
                            <h3 className="mr-auto">镜头 {i + 1}</h3>
                            <button type="button" aria-label={"镜头" + (i + 1) + "上移"} disabled={i === 0} onClick={() => move(i, -1)}>
                                上移
                            </button>
                            <button type="button" aria-label={"镜头" + (i + 1) + "下移"} disabled={i === value.shots.length - 1} onClick={() => move(i, 1)}>
                                下移
                            </button>
                            <button
                                type="button"
                                onClick={() => {
                                    if (window.confirm("移除这个镜头及其文案？保存后生效。")) onChange({ ...value, shots: value.shots.filter((row) => row.id !== s.id) });
                                }}
                            >
                                移除镜头
                            </button>
                        </div>
                        <div className="grid gap-3 sm:grid-cols-2">
                            <label>
                                时长（秒）
                                <input aria-label={"镜头" + (i + 1) + "时长（秒）"} type="number" min={1} max={15} step={0.001} value={s.durationMs / 1000} onChange={(e) => shot(s.id, { durationMs: Math.round(Number(e.target.value) * 1000) })} />
                            </label>
                            <label>
                                画面描述
                                <textarea maxLength={1000} value={s.visual} onChange={(e) => shot(s.id, { visual: e.target.value })} />
                            </label>
                            <label>
                                旁白
                                <textarea maxLength={500} value={s.voiceover} onChange={(e) => shot(s.id, { voiceover: e.target.value })} />
                            </label>
                            <label>
                                屏幕文字（最多两行）
                                <textarea rows={2} maxLength={200} value={s.onScreenText} onChange={(e) => shot(s.id, { onScreenText: e.target.value })} />
                            </label>
                        </div>
                        <fieldset>
                            <legend>参考素材（最多5个）</legend>
                            {pool.length === 0 ? (
                                <p className="text-muted-foreground">先在素材区绑定商品或附加素材。</p>
                            ) : (
                                pool.map((id) => (
                                    <label key={id} className="ad-check">
                                        <input
                                            type="checkbox"
                                            checked={s.referenceResourceIds.includes(id)}
                                            disabled={!s.referenceResourceIds.includes(id) && s.referenceResourceIds.length >= 5}
                                            onChange={(e) => shot(s.id, { referenceResourceIds: e.target.checked ? [...s.referenceResourceIds, id] : s.referenceResourceIds.filter((r) => r !== id) })}
                                        />
                                        {id}
                                    </label>
                                ))
                            )}
                        </fieldset>
                    </article>
                ))}
                <button type="button" disabled={value.shots.length >= 5} onClick={() => onChange({ ...value, shots: [...value.shots, { id: crypto.randomUUID(), durationMs: 5000, visual: "", voiceover: "", onScreenText: "", referenceResourceIds: [] }] })}>
                    添加镜头
                </button>
            </section>
        </fieldset>
    );
}
