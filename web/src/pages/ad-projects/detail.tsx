import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useBlocker, useParams } from "react-router";
import { Button } from "antd";
import { WorkspacePage, PageHeader, PaginationBar } from "@/components/layout/workspace-page";
import { AppModal } from "@/components/ui/product/app-modal";
import { useUserStore } from "@/stores/use-user-store";
import { getActiveUserScope } from "@/lib/user-scope";
import { ApiError } from "@/services/api/request";
import { ADVERTISING_REASONS } from "@/services/api/error-codes";
import * as api from "@/services/api/advertising-project";
import { AdvertisingDraftForm } from "./advertising-draft-form";
import { AdvertisingAssets } from "./advertising-assets";
import { AdvertisingProduction } from "./advertising-production";
import "./advertising.css";

export default function AdvertisingDetailPage() {
    const { id } = useParams();
    const userId = useUserStore((s) => s.user?.id);
    return id && userId ? <AdvertisingEditor key={userId + ":" + id} id={id} userId={userId} /> : <p role="alert">请先登录并选择广告项目。</p>;
}
export function AdvertisingEditor({ id, userId }: { id: string; userId: string }) {
    const client = useQueryClient();
    const key = ["advertising", userId, id];
    const query = useQuery({ queryKey: key, queryFn: ({ signal }) => api.getAdvertisingProject(id, signal), refetchOnWindowFocus: false, retry: false });
    const [saved, setSaved] = useState<api.AdvertisingDetail>();
    const [draft, setDraft] = useState<api.AdvertisingDraft>();
    const [name, setName] = useState("");
    const [busy, setBusy] = useState(false);
    const [uploading, setUploading] = useState(false);
    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");
    const [remote, setRemote] = useState<api.AdvertisingDetail>();
    const [conflict, setConflict] = useState(false);
    const [confirmOpen, setConfirmOpen] = useState(false);
    const [historyId, setHistoryId] = useState("");
    const [page, setPage] = useState(1);
    const live = useRef(true);
    const writing = useRef(false);
    const scope = useRef(getActiveUserScope());
    useEffect(() => {
        live.current = true;
        return () => {
            live.current = false;
        };
    }, []);
    const current = () => live.current && getActiveUserScope() === scope.current && useUserStore.getState().user?.id === userId;
    const adopt = (data: api.AdvertisingDetail) => {
        setSaved(data);
        setDraft(data.draft);
        setName(data.project.name);
        setRemote(undefined);
        setConflict(false);
        client.setQueryData(key, data);
    };
    const dirty = !!saved && !!draft && (name !== saved.project.name || JSON.stringify(draft) !== JSON.stringify(saved.draft));
    useEffect(() => {
        if (query.data && query.data !== saved && !dirty && !busy && !uploading && !conflict && (!saved || query.data.revision >= saved.revision)) adopt(query.data);
    }, [query.data, saved, dirty, busy, uploading, conflict]);
    const guard = dirty || busy || uploading;
    const blocker = useBlocker(guard);
    useEffect(() => {
        const unload = (event: BeforeUnloadEvent) => {
            if (guard) {
                event.preventDefault();
                event.returnValue = "";
            }
        };
        window.addEventListener("beforeunload", unload);
        return () => window.removeEventListener("beforeunload", unload);
    }, [guard]);
    const revisions = useQuery({ queryKey: [...key, "revisions", page], queryFn: ({ signal }) => api.listAdvertisingRevisions(id, page, 20, signal), enabled: !!saved, retry: false });
    const latest = useQuery({
        queryKey: [...key, "history", saved?.latestConfirmedRevisionId],
        queryFn: ({ signal }) => api.getAdvertisingRevision(id, saved!.latestConfirmedRevisionId!, signal),
        enabled: !!saved?.latestConfirmedRevisionId,
        retry: false,
    });
    const differsFromConfirmed = !!latest.data && (name !== latest.data.snapshot.name || JSON.stringify(draft) !== JSON.stringify(latest.data.snapshot.draft));
    const history = useQuery({ queryKey: [...key, "history", historyId], queryFn: ({ signal }) => api.getAdvertisingRevision(id, historyId, signal), enabled: !!historyId, retry: false });
    const readRemote = async () => {
        try {
            const data = await api.getAdvertisingProject(id);
            if (current()) setRemote(data);
        } catch (e) {
            if (current()) setError("读取服务端版本失败；本地输入仍保留。" + (e instanceof Error ? e.message : ""));
        }
    };
    const write = async (confirm: boolean) => {
        if (!saved || !draft || writing.current || uploading || !current() || (confirm && dirty)) return;
        writing.current = true;
        setBusy(true);
        setError("");
        setNotice("");
        try {
            if (confirm) {
                const result = await api.confirmAdvertisingProject(id, saved.revision);
                if (!current()) return;
                const latest = await api.getAdvertisingProject(id);
                if (!current()) return;
                adopt(latest);
                setHistoryId(result.snapshot.id);
                setNotice("已确认并冻结版本；继续编辑不会改变这份历史。");
                setConfirmOpen(false);
                await client.invalidateQueries({ queryKey: [...key, "revisions"] });
            } else {
                const result = await api.saveAdvertisingProject(id, { name, draft, expectedRevision: saved.revision });
                if (!current()) return;
                adopt(result);
                setNotice("已保存到服务端");
            }
            void client.invalidateQueries({ queryKey: ["advertising-list", userId] });
        } catch (e) {
            if (!current()) return;
            setError(e instanceof Error ? e.message : "操作失败，输入已保留");
            if (e instanceof ApiError && e.reason === ADVERTISING_REASONS.revisionConflict) {
                setConflict(true);
                await readRemote();
            }
        } finally {
            writing.current = false;
            if (current()) setBusy(false);
        }
    };
    return (
        <WorkspacePage className="ad-workspace">
            <PageHeader title="广告脚本工作台" description="需求 → 手工脚本 → 确认版本。本阶段不生成视频、不触发模型费用。" actions={<Link to="/ad-projects">返回广告项目</Link>} />
            {!saved || !draft ? (
                <div role={query.error ? "alert" : "status"}>
                    {query.error ? String(query.error) : "正在读取广告…"}
                    {query.error && <Button onClick={() => void query.refetch()}>重试</Button>}
                </div>
            ) : (
                <div className="space-y-5 mt-4">
                    <section className="ad-section">
                        <label>
                            项目名称
                            <input aria-label="项目名称" maxLength={120} value={name} disabled={busy || uploading} onChange={(e) => setName(e.target.value)} />
                        </label>
                        <div className="flex flex-wrap gap-3 items-center mt-3">
                            <span role="status">
                                修订 {saved.revision} · {dirty ? "有未保存修改" : "已与服务端同步"}
                            </span>
                            <Button type="primary" loading={busy} disabled={uploading || conflict} onClick={() => void write(false)}>
                                保存草稿
                            </Button>
                            <Button disabled={dirty || busy || uploading || conflict} onClick={() => setConfirmOpen(true)}>
                                确认脚本
                            </Button>
                            {dirty && <span>先保存，才能确认。</span>}
                        </div>
                    </section>
                    {error && <p role="alert">{error}</p>}
                    {notice && <p role="status">{notice}</p>}
                    {conflict && (
                        <section className="ad-section" aria-label="版本冲突">
                            <h2>服务端已有新版本，本地输入未覆盖</h2>
                            <p>请对照内容后选择。保留本地会以新的修订号再次保存，不会自动覆盖。</p>
                            <Button disabled={busy} onClick={() => void readRemote()}>
                                重新读取服务端版本
                            </Button>
                            {remote && (
                                <>
                                    <p>
                                        服务端修订 {remote.revision} · {remote.project.name}
                                    </p>
                                    <details>
                                        <summary>查看服务端内容</summary>
                                        <pre className="whitespace-pre-wrap break-all">{JSON.stringify(remote.draft, null, 2)}</pre>
                                    </details>
                                    <Button
                                        disabled={busy}
                                        onClick={() => {
                                            if (window.confirm("放弃本地修改，采用服务端版本？")) adopt(remote);
                                        }}
                                    >
                                        采用服务端版本
                                    </Button>
                                    <Button
                                        disabled={busy}
                                        onClick={() => {
                                            setSaved(remote);
                                            setConflict(false);
                                            setRemote(undefined);
                                            setError("");
                                        }}
                                    >
                                        保留本地内容继续编辑
                                    </Button>
                                </>
                            )}
                        </section>
                    )}
                    <AdvertisingAssets value={draft} onChange={setDraft} resources={saved.resources} disabled={busy} onBusy={setUploading} />
                    <AdvertisingDraftForm value={draft} onChange={setDraft} disabled={busy || uploading} />
                    <AdvertisingProduction key={saved.latestConfirmedRevisionId || "unconfirmed"} projectId={id} userId={userId} confirmedRevisionId={saved.latestConfirmedRevisionId} />
                    <section className="ad-section">
                        <h2>确认历史</h2>
                        <p>确认快照只读，不包含临时签名URL。</p>
                        {differsFromConfirmed && <p role="status">当前草稿与最新确认版本不同；历史确认仍然有效。</p>}
                        {latest.error && <p role="alert">暂时无法比较最新确认版本，请稍后重试。</p>}
                        {revisions.error ? (
                            <p role="alert">{String(revisions.error)}</p>
                        ) : revisions.data?.items.length === 0 ? (
                            <p>尚未确认。保存脚本后点击“确认脚本”。</p>
                        ) : (
                            <ul>
                                {revisions.data?.items.map((v) => (
                                    <li key={v.id}>
                                        <Button onClick={() => setHistoryId(v.id)}>
                                            来源修订 {v.sourceRevision} · {new Date(v.createdAt).toLocaleString()}
                                        </Button>
                                        {v.id === saved.latestConfirmedRevisionId && "最新确认"}
                                    </li>
                                ))}
                            </ul>
                        )}
                        <PaginationBar current={page} pageSize={20} total={revisions.data?.total || 0} pageSizeOptions={[20]} onChange={setPage} />
                    </section>
                </div>
            )}
            <AppModal
                rootClassName="ad-workspace"
                open={confirmOpen}
                title="确认当前已保存脚本？"
                okText="确认并冻结"
                cancelText="继续编辑"
                confirmLoading={busy}
                onOk={() => void write(true)}
                onCancel={() => {
                    if (!busy) setConfirmOpen(false);
                }}
                okButtonProps={{ disabled: dirty || uploading || conflict }}
            >
                <p>冻结需求、镜头顺序和资源元数据。不会生成或收费；后续修改不影响历史。</p>
                {error && <p role="alert">{error}</p>}
            </AppModal>
            <AppModal rootClassName="ad-workspace" open={!!historyId} title="只读确认版本" footer={null} onCancel={() => setHistoryId("")} width={900}>
                {history.error ? (
                    <p role="alert">{String(history.error)}</p>
                ) : history.data ? (
                    <>
                        <h2>{history.data.snapshot.name}</h2>
                        <p>来源修订 {history.data.sourceRevision}</p>
                        <AdvertisingDraftForm value={history.data.snapshot.draft} onChange={() => {}} disabled />
                        <AdvertisingProduction key={history.data.id} projectId={id} userId={userId} confirmedRevisionId={history.data.id} />
                        <h3>冻结资源</h3>
                        <ul>
                            {history.data.snapshot.resources.map((r) => (
                                <li key={r.id}>
                                    {r.title} · {r.mimeType} · {r.width}×{r.height} · {r.durationMs}ms
                                </li>
                            ))}
                        </ul>
                    </>
                ) : (
                    <p role="status">读取确认版本…</p>
                )}
            </AppModal>
            <AppModal
                rootClassName="ad-workspace"
                open={blocker.state === "blocked"}
                title="离开广告编辑？"
                okText="放弃并离开"
                cancelText="继续编辑"
                onOk={() => blocker.state === "blocked" && blocker.proceed()}
                onCancel={() => blocker.state === "blocked" && blocker.reset()}
            >
                <p>未保存的输入会丢失；已发出的操作可能仍在服务端完成。回来后请重新读取。</p>
            </AppModal>
        </WorkspacePage>
    );
}
