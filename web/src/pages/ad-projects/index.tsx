import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router";
import { Button } from "antd";
import { AppModal } from "@/components/ui/product/app-modal";
import { WorkspacePage, PageHeader, PaginationBar } from "@/components/layout/workspace-page";
import { useUserStore } from "@/stores/use-user-store";
import * as api from "@/services/api/advertising-project";
import "./advertising.css";

export default function AdvertisingProjectsPage() {
    const userId = useUserStore((s) => s.user?.id);
    return userId ? <AdvertisingList key={userId} userId={userId} /> : <p role="alert">请先登录。</p>;
}
function AdvertisingList({ userId }: { userId: string }) {
    const [page, setPage] = useState(1);
    const [size, setSize] = useState(20);
    const [createOpen, setCreateOpen] = useState(false);
    const [name, setName] = useState("");
    const [remove, setRemove] = useState<api.AdvertisingListItem>();
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const live = useRef(true);
    const writing = useRef(false);
    const navigate = useNavigate();
    const client = useQueryClient();
    useEffect(() => {
        live.current = true;
        return () => {
            live.current = false;
        };
    }, []);
    const current = () => live.current && useUserStore.getState().user?.id === userId;
    const query = useQuery({ queryKey: ["advertising-list", userId, page, size], queryFn: ({ signal }) => api.listAdvertisingProjects(page, size, signal), retry: false });
    const submit = async () => {
        if (writing.current || !current()) return;
        writing.current = true;
        setBusy(true);
        setError("");
        try {
            if (remove) {
                await api.deleteAdvertisingProject(remove.id, remove.revision);
                if (!current()) return;
                setRemove(undefined);
                await client.invalidateQueries({ queryKey: ["advertising-list", userId] });
            } else {
                const result = await api.createAdvertisingProject(name);
                if (!current()) return;
                void client.invalidateQueries({ queryKey: ["advertising-list", userId] });
                navigate("/ad-projects/" + result.project.id);
            }
        } catch (e) {
            if (current()) setError(e instanceof Error ? e.message : "操作失败");
        } finally {
            writing.current = false;
            if (current()) setBusy(false);
        }
    };
    return (
        <WorkspacePage className="ad-workspace">
            <PageHeader
                title="广告项目"
                description="从商品需求开始，编写15–30秒竖屏广告脚本，确认后保留不可变版本。"
                actions={
                    <Button
                        type="primary"
                        onClick={() => {
                            setCreateOpen(true);
                            setError("");
                        }}
                    >
                        新建广告项目
                    </Button>
                }
            />
            {query.isPending ? (
                <p role="status">正在读取广告项目…</p>
            ) : query.error ? (
                <p role="alert">
                    {String(query.error)}
                    <Button onClick={() => void query.refetch()}>重试</Button>
                </p>
            ) : query.data.items.length === 0 ? (
                <section className="ad-section mt-4">
                    <h2>先确定要讲清楚的一件事</h2>
                    <p>新建广告项目，添加商品图、受众和卖点，再手工编写分镜。当前阶段不生成视频。</p>
                </section>
            ) : (
                <ul className="grid gap-4 mt-4 md:grid-cols-2 xl:grid-cols-3">
                    {query.data.items.map((item) => (
                        <li key={item.id} className="ad-section">
                            <h2>
                                <Link to={"/ad-projects/" + item.id}>{item.name}</Link>
                            </h2>
                            <p>
                                {item.productName || "尚未填写商品"} · {item.durationMs / 1000}秒
                            </p>
                            <p>
                                {item.latestConfirmedRevisionId ? "已有确认版本" : "草稿"} · 修订 {item.revision}
                            </p>
                            <p className="text-muted-foreground">更新于 {new Date(item.updatedAt).toLocaleString()}</p>
                            <div className="flex gap-3">
                                <Link to={"/ad-projects/" + item.id}>打开脚本</Link>
                                <Button
                                    danger
                                    onClick={() => {
                                        setRemove(item);
                                        setError("");
                                    }}
                                >
                                    删除
                                </Button>
                            </div>
                        </li>
                    ))}
                </ul>
            )}
            <PaginationBar
                current={page}
                pageSize={size}
                total={query.data?.total || 0}
                onChange={(p, s) => {
                    setPage(p);
                    setSize(s);
                }}
            />
            <AppModal
                rootClassName="ad-workspace"
                open={createOpen || !!remove}
                title={remove ? "删除广告项目？" : "新建广告项目"}
                okText={remove ? "删除项目" : "创建"}
                cancelText="取消"
                confirmLoading={busy}
                okButtonProps={{ danger: !!remove, disabled: !remove && !name.trim() }}
                onOk={() => void submit()}
                onCancel={() => {
                    if (!busy) {
                        setCreateOpen(false);
                        setRemove(undefined);
                    }
                }}
            >
                {remove ? (
                    <p>将删除“{remove.name}”及全部确认版本，无法撤销。独立媒体不会被删除，画布会解绑。项目有新修订或活动任务时删除会被拒绝。</p>
                ) : (
                    <label>
                        项目名称
                        <input aria-label="新广告项目名称" maxLength={120} value={name} onChange={(e) => setName(e.target.value)} disabled={busy} />
                    </label>
                )}
                {error && <p role="alert">{error}</p>}
                {remove && error && (
                    <Button
                        disabled={busy}
                        onClick={() => {
                            setRemove(undefined);
                            void query.refetch();
                        }}
                    >
                        取消删除并刷新列表
                    </Button>
                )}
            </AppModal>
        </WorkspacePage>
    );
}
