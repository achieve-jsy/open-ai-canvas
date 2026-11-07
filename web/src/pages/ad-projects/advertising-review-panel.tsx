import { advertisingCandidateLabel } from "./advertising-candidate-label";
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "antd";
import { AppDrawer } from "@/components/ui/product/app-drawer";
import { PaginationBar } from "@/components/layout/workspace-page";
import { getActiveUserScope } from "@/lib/user-scope";
import * as api from "@/services/api/advertising-generation";

export function AdvertisingReviewPanel({
    projectId,
    production,
    current,
    onRework,
    onBusy,
}: {
    projectId: string;
    production: api.AdvertisingProductionDetail;
    current: () => boolean;
    onRework: (issue: api.AdvertisingIssue, submission: api.AdvertisingSubmission) => void;
    onBusy: (id: string, busy: boolean) => void;
}) {
    const [open, setOpen] = useState(false);
    const [shotId, setShot] = useState("");
    const [kind, setKind] = useState("");
    const [eventPage, setEventPage] = useState(1);
    const [issuePage, setIssuePage] = useState(1);
    const [reviewPage, setReviewPage] = useState(1);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const [resolution, setResolution] = useState("");
    const p = production.production.id;
    const key = ["advertising-candidates", getActiveUserScope(), projectId, p];
    const events = useQuery({ queryKey: [...key, "events", shotId, kind, eventPage], queryFn: ({ signal }) => api.listAdvertisingEvents(projectId, { productionId: p, shotId, kind, page: eventPage, pageSize: 20 }, signal), enabled: open, retry: false });
    const issues = useQuery({ queryKey: [...key, "issues", issuePage], queryFn: ({ signal }) => api.listAdvertisingIssues(projectId, p, issuePage, 20, signal), enabled: open, retry: false });
    const reviews = useQuery({ queryKey: [...key, "reviews", reviewPage], queryFn: ({ signal }) => api.listAdvertisingReviews(projectId, p, reviewPage, 20, signal), enabled: open, retry: false });
    useEffect(() => {
        onBusy("audit", busy);
    }, [busy, onBusy]);
    useEffect(() => () => onBusy("audit", false), [onBusy]);
    const run = async (action: () => Promise<void>) => {
        if (busy || !current()) return;
        setBusy(true);
        setError("");
        try {
            await action();
        } catch (e) {
            if (current()) setError(e instanceof Error ? e.message : "读取失败");
        } finally {
            if (current()) setBusy(false);
        }
    };
    const rework = (issue: api.AdvertisingIssue) =>
        run(async () => {
            if (!issue.submissionId) throw new Error("成片问题请在成片阶段处理");
            // No direct submission GET exists: page original history until the exact server identity is found.
            let page = 1;
            while (current()) {
                const history = await api.listAdvertisingSubmissions(projectId, p, page, 100);
                const submission = history.items.find((s) => s.id === issue.submissionId);
                if (submission) {
                    if (current()) {
                        onRework(issue, submission);
                        setOpen(false);
                    }
                    return;
                }
                if (page * 100 >= history.total || !history.items.length) break;
                page++;
            }
            throw new Error("原候选暂不可读取，未创建新批次");
        });
    const kinds: Record<string, string> = {
        script_confirmed: "脚本确认",
        submission_admitted: "生成授权",
        candidate_output: "候选输出",
        candidate_reviewed: "候选验收",
        selection_changed: "采用变化",
        issue_created: "问题记录",
        issue_resolved: "问题处理",
        assembly_created: "组装",
        render_created: "导出开始",
        render_output: "导出结果",
        assembly_reviewed: "成片验收",
        delivery_reviewed: "交付确认",
        task_created_fact: "历史任务创建",
        task_result_fact: "历史任务结果",
    };
    return (
        <>
            <Button onClick={() => setOpen(true)}>审计与问题</Button>
            <AppDrawer
                rootClassName="ad-workspace"
                open={open}
                title="制作事件、验收与问题"
                onClose={() => {
                    if (!busy) setOpen(false);
                }}
                size="large"
            >
                <div className="ad-production-grid">
                    <label>
                        镜头筛选
                        <select
                            value={shotId}
                            onChange={(e) => {
                                setShot(e.target.value);
                                setEventPage(1);
                            }}
                        >
                            <option value="">全部镜头</option>
                            {production.confirmedSnapshot.snapshot.draft.shots.map((s, i) => (
                                <option key={s.id} value={s.id}>
                                    镜头{i + 1}
                                </option>
                            ))}
                        </select>
                    </label>
                    <label>
                        环节筛选
                        <select
                            value={kind}
                            onChange={(e) => {
                                setKind(e.target.value);
                                setEventPage(1);
                            }}
                        >
                            <option value="">全部环节</option>
                            {Object.entries(kinds).map(([id, label]) => (
                                <option key={id} value={id}>
                                    {label}
                                </option>
                            ))}
                        </select>
                    </label>
                </div>
                {(error || events.error || issues.error || reviews.error) && <p role="alert">{error || String(events.error || issues.error || reviews.error)}</p>}
                <Button
                    onClick={() => {
                        void events.refetch();
                        void reviews.refetch();
                        void issues.refetch();
                    }}
                >
                    刷新审计记录
                </Button>
                <h3>真实事件</h3>
                {events.isPending ? <p role="status">正在读取事件…</p> : !events.data?.items.length && !events.error && <p>暂无匹配事件。</p>}
                {events.data?.items.map((e) => (
                    <article className="ad-candidate" key={e.id}>
                        {e.submissionId && <p>候选 {advertisingCandidateLabel(e.submissionId)}</p>}
                        <p>
                            {kinds[e.kind] || e.kind} · {e.fact ? "原任务事实（非人工批准）" : "服务端事件"} · {new Date(e.createdAt).toLocaleString()}
                        </p>
                        <p>
                            {e.details.decision} {e.details.status} {e.details.category} {e.details.note}
                        </p>
                        {e.details.atMs !== undefined && <p>时间点 {e.details.atMs}ms</p>}
                        <details>
                            <summary>技术标识</summary>
                            <p>
                                事件 {e.id} · 镜头 {e.shotId} · 候选 {e.submissionId} · 组装 {e.assemblyId} · 导出 {e.renderAttemptId} · 任务 {e.details.taskId}
                            </p>
                        </details>
                    </article>
                ))}
                <PaginationBar current={eventPage} pageSize={20} total={events.data?.total || 0} pageSizeOptions={[20]} onChange={setEventPage} />
                <h3>人工验收</h3>
                {!reviews.isPending && !reviews.data?.items.length && !reviews.error && <p>暂无人工验收记录。</p>}
                {reviews.data?.items.map((r) => (
                    <article className="ad-candidate" key={r.id}>
                        {r.submissionId && <p>候选 {advertisingCandidateLabel(r.submissionId)}</p>}
                        <p>
                            {r.checkpoint === "candidate" ? "候选" : r.checkpoint === "delivery" ? "交付" : "成片"} · {r.decision === "approve" ? "批准" : "淘汰"} · {r.reason}
                        </p>
                        <p>{new Date(r.createdAt).toLocaleString()}</p>
                        <details>
                            <summary>验收标识</summary>
                            <p>
                                {r.id} · 候选 {r.submissionId} · 组装 {r.assemblyId} · 导出 {r.renderAttemptId}
                            </p>
                        </details>
                    </article>
                ))}
                <PaginationBar current={reviewPage} pageSize={20} total={reviews.data?.total || 0} pageSizeOptions={[20]} onChange={setReviewPage} />
                <h3>问题</h3>
                <p>返工仅预填建议，不生成也不自动解决原问题。</p>
                <label>
                    处理说明
                    <textarea aria-label="问题处理说明" value={resolution} maxLength={2000} onChange={(e) => setResolution(e.target.value)} />
                </label>
                {!issues.isPending && !issues.data?.items.length && !issues.error && <p>暂无问题记录。</p>}
                {issues.data?.items.map((issue) => (
                    <article className="ad-candidate" key={issue.id}>
                        {issue.submissionId && <p>候选 {advertisingCandidateLabel(issue.submissionId)}</p>}
                        <p>
                            {issue.category} · {issue.status === "open" ? "待处理" : "已解决"} · {issue.note}
                        </p>
                        {issue.atMs !== undefined && <p>时间点 {issue.atMs}ms</p>}
                        <p>{issue.resolutionNote}</p>
                        <details>
                            <summary>问题标识</summary>
                            <p>
                                {issue.id} · 候选 {issue.submissionId} · 组装 {issue.assemblyId} · 导出 {issue.renderAttemptId}
                            </p>
                        </details>
                        <div className="ad-actions">
                            {issue.submissionId && (
                                <Button disabled={busy} onClick={() => void rework(issue)}>
                                    以此问题准备返工
                                </Button>
                            )}
                            <Button
                                disabled={busy || !resolution.trim()}
                                onClick={() =>
                                    void run(async () => {
                                        await api.resolveAdvertisingIssue(projectId, p, issue.id, { expectedRevision: issue.revision, status: issue.status === "open" ? "resolved" : "open", resolutionNote: resolution });
                                        if (current()) {
                                            setResolution("");
                                            await issues.refetch();
                                            await events.refetch();
                                        }
                                    })
                                }
                            >
                                {issue.status === "open" ? "明确解决问题" : "重新打开问题"}
                            </Button>
                        </div>
                    </article>
                ))}
                <PaginationBar current={issuePage} pageSize={20} total={issues.data?.total || 0} pageSizeOptions={[20]} onChange={setIssuePage} />
            </AppDrawer>
        </>
    );
}
