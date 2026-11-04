import { lazy, Suspense, useEffect, useRef, useState } from "react";
import type { Asset } from "@/stores/use-asset-store";
import type { AdvertisingDraft, AdvertisingResource } from "@/services/api/advertising-project";
import { resourceIdFromStorageKey, resourceFileUrl } from "@/services/api/resources";
import { uploadMediaFile } from "@/services/file-storage";
import { getActiveUserScope } from "@/lib/user-scope";

const Picker = lazy(() => import("@/components/assets/asset-library-picker-modal").then((m) => ({ default: m.AssetLibraryPickerModal })));
const images = ["image/png", "image/jpeg", "image/webp"];
const media = [...images, "video/mp4", "audio/mpeg", "audio/mp4", "audio/wav", "audio/x-wav"];
type Pool = "productResourceIds" | "additionalResourceIds" | "logo";
export function AdvertisingAssets({ value, onChange, resources, disabled, onBusy }: { value: AdvertisingDraft; onChange: (value: AdvertisingDraft) => void; resources: AdvertisingResource[]; disabled: boolean; onBusy: (busy: boolean) => void }) {
    const [pool, setPool] = useState<Pool>();
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const current = useRef({ value, onChange, onBusy });
    current.current = { value, onChange, onBusy };
    const alive = useRef(true);
    useEffect(() => {
        alive.current = true;
        return () => {
            alive.current = false;
        };
    }, []);
    const allowed = (target: Pool) => (target === "additionalResourceIds" ? media : images);
    const assetReason = (asset: Asset) => {
        if (!("storageKey" in asset.data) || !resourceIdFromStorageKey(asset.data.storageKey)) return "仅本地，尚未可绑定";
        if (!("mimeType" in asset.data) || !allowed(pool!).includes(asset.data.mimeType.toLowerCase())) return "素材格式不支持";
        return undefined;
    };
    const bind = (target: Pool, ids: string[]) => {
        const v = current.current.value;
        const unique = [...new Set(ids)];
        if (target === "logo") {
            if (unique.length !== 1) throw new Error("请选择一个Logo");
            current.current.onChange({ ...v, brand: { ...v.brand, logoResourceId: unique[0] } });
            return;
        }
        const merged = [...new Set([...v[target], ...unique])];
        if (merged.length > (target === "productResourceIds" ? 10 : 20)) throw new Error("已超过素材数量上限");
        current.current.onChange({ ...v, [target]: merged });
    };
    const remove = (target: Pool, id: string) => {
        const v = current.current.value;
        if (target === "logo") {
            onChange({ ...v, brand: { ...v.brand, logoResourceId: "" } });
            return;
        }
        const next = { ...v, [target]: v[target].filter((r) => r !== id) };
        const remaining = new Set([...next.productResourceIds, ...next.additionalResourceIds]);
        const linked = v.shots.flatMap((s, index) => (s.referenceResourceIds.includes(id) ? [index + 1] : []));
        if (!remaining.has(id) && linked.length) {
            setError("请先在镜头中取消引用：" + linked.join("、"));
            return;
        }
        setError("");
        onChange(next);
    };
    const upload = async (target: Pool, file?: File) => {
        if (!file || busy || disabled) return;
        setError("");
        if (!allowed(target).includes(file.type.toLowerCase())) {
            setError("文件格式不支持，请选择标注的格式。");
            return;
        }
        const scope = getActiveUserScope();
        setBusy(true);
        onBusy(true);
        try {
            const result = await uploadMediaFile(file, "advertising");
            if (!alive.current || scope !== getActiveUserScope()) return;
            const id = resourceIdFromStorageKey(result.storageKey);
            if (result.pendingRemoteUpload || !id) {
                setError("仅本地，尚未可绑定。文件已保留在本机；请网络恢复后重新上传或从已同步素材库选择。");
                return;
            }
            if (!allowed(target).includes(result.mimeType.toLowerCase())) throw new Error("服务端识别的文件格式不支持");
            bind(target, [id]);
        } catch (e) {
            if (alive.current && scope === getActiveUserScope()) setError(e instanceof Error ? e.message : "上传失败");
        } finally {
            if (alive.current) {
                setBusy(false);
                current.current.onBusy(false);
            }
        }
    };
    return (
        <section className="ad-section">
            <h2>商品与参考素材</h2>
            <p className="text-muted-foreground">确认至少需要1张商品图。移除绑定不会删除原素材，历史确认引用仍受保护。</p>
            {error && <p role="alert">{error}</p>}
            {(["productResourceIds", "logo", "additionalResourceIds"] as const).map((target) => (
                <fieldset key={target} disabled={disabled || busy} className="space-y-2">
                    <legend>{target === "logo" ? "品牌Logo（1张）" : target === "productResourceIds" ? "商品图（最多10张）" : "附加素材（最多20个）"}</legend>
                    <div className="flex flex-wrap gap-3">
                        <button type="button" onClick={() => setPool(target)}>
                            从素材库选择{target === "logo" ? "Logo" : target === "productResourceIds" ? "商品图" : "附加素材"}
                        </button>
                        <label>
                            上传{target === "logo" ? "Logo" : target === "productResourceIds" ? "商品图" : "附加素材"}
                            <input
                                type="file"
                                accept={allowed(target).join(",")}
                                onChange={(e) => {
                                    void upload(target, e.target.files?.[0]);
                                    e.target.value = "";
                                }}
                            />
                        </label>
                    </div>
                    <p className="text-muted-foreground">{target === "additionalResourceIds" ? "PNG / JPEG / WebP / MP4 / MP3 / M4A / WAV" : "PNG / JPEG / WebP"}</p>
                    <ul className="space-y-2">
                        {(target === "logo" ? (value.brand.logoResourceId ? [value.brand.logoResourceId] : []) : value[target]).map((id) => {
                            const resource = resources.find((r) => r.id === id);
                            return (
                                <li key={id} className="flex flex-wrap gap-3 items-center break-all">
                                    <a href={resourceFileUrl(id)} target="_blank" rel="noreferrer">
                                        {resource?.title || id}
                                    </a>
                                    {resource && !resource.available && <span role="status">不可用，请移除或替换</span>}
                                    <button type="button" onClick={() => remove(target, id)}>
                                        移除绑定
                                    </button>
                                </li>
                            );
                        })}
                    </ul>
                </fieldset>
            ))}
            {busy && <p role="status">正在上传，请勿离开…</p>}
            {pool && (
                <Suspense fallback={<p role="status">正在打开素材库…</p>}>
                    <Picker
                        open
                        remoteLibrary
                        acceptRemoteItems
                        items={[]}
                        categoryLabels={{ all: "全部", material: "素材" }}
                        remoteKind={pool === "additionalResourceIds" ? undefined : "image"}
                        mediaKinds={pool === "additionalResourceIds" ? ["image", "video", "audio"] : ["image"]}
                        multiple={pool !== "logo"}
                        remoteItemDisabledReason={assetReason}
                        onClose={() => setPool(undefined)}
                        onConfirm={(_, items) => {
                            try {
                                const ids = (items || []).map((item) => {
                                    if (!item.asset || assetReason(item.asset)) throw new Error("素材尚未可绑定或类型不符");
                                    return resourceIdFromStorageKey((item.asset.data as { storageKey?: string }).storageKey);
                                });
                                if (!ids.length) throw new Error("请选择素材");
                                bind(pool, ids);
                                setPool(undefined);
                                setError("");
                            } catch (e) {
                                setError(e instanceof Error ? e.message : "绑定失败");
                            }
                        }}
                    />
                </Suspense>
            )}
        </section>
    );
}
