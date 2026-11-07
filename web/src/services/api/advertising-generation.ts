import { compactApiParams, http } from "./request";
import type { AdvertisingPage, AdvertisingRevisionDetail } from "./advertising-project";
import type { GenerationTask } from "./task-center";
import type { CreationSubmission } from "./creation-runs";
import type { TimelineProject } from "@/types/timeline";

export type AdvertisingSelection = { videoSubmissionId: string; audioSubmissionId: string; videoStartMs: number };
export type AdvertisingProductionSettings = { fit: "pad" | "crop"; ttsModelId: string; voiceId: string; selections: Record<string, AdvertisingSelection>; musicResourceId: string; musicVolume: number };
export type AdvertisingProductionDetail = {
    production: { id: string; projectId: string; confirmedRevisionId: string; revision: number; createdAt: string; updatedAt: string };
    settings: AdvertisingProductionSettings;
    confirmedSnapshot: AdvertisingRevisionDetail;
};
export type AdvertisingSubmissionRequest = { clientOperationId: string; shotId: string; kind: "video" | "audio"; modelId: string; voiceId: string; format: string };
export type AdvertisingSubmission = {
    id: string;
    productionId: string;
    shotId: string;
    kind: "video" | "audio";
    batchId?: string;
    candidateIndex?: number;
    requestHash: string;
    publicRequest: { prompt: string; modelId: string; voiceId?: string; format?: string; speed?: number; durationSeconds?: number; size?: string; resolution?: string; referenceResourceIds?: string[] };
    quote: CreationSubmission["quote"];
    task?: GenerationTask;
};
export type AdvertisingSourceSettings = Pick<AdvertisingProductionSettings, "selections" | "musicResourceId" | "musicVolume" | "fit">;
export type AdvertisingCutSource = { key: string; taskId?: string; resourceId: string; signature: string; durationMs: number; width: number; height: number };
export type AdvertisingAssembly = {
    latestReview?: AdvertisingReview;
    sourceFingerprint?: string;
    sourceSettings?: AdvertisingSourceSettings;
    sources?: AdvertisingCutSource[];
    current: boolean;
    staleReason?: string;
    id: string;
    productionId: string;
    clientOperationId: string;
    sourceProductionRevision: number;
    selectionHash: string;
    timeline: TimelineProject;
    canvasId?: string;
    createdAt: string;
    updatedAt: string;
};
export type AdvertisingRender = { id: string; assemblyId: string; clientOperationId: string; taskId: string; createdAt: string; task: GenerationTask; resourceId?: string; available: boolean };
export type AdvertisingBatchRequest = Omit<AdvertisingSubmissionRequest, "voiceId" | "format"> & { count: 1 | 2 | 3; adjustment: string; voiceId?: string; format?: string };
export type AdvertisingBatchApproval = { submissionId: string; quoteHash: string };
export type AdvertisingCandidateBatch = {
    id: string;
    productionId: string;
    clientOperationId: string;
    shotId: string;
    kind: "video" | "audio";
    count: 1 | 2 | 3;
    adjustment: string;
    requestHash: string;
    approvedAt?: string;
    approvalHash?: string;
    closedAt?: string;
    createdAt: string;
    updatedAt: string;
};
export type AdvertisingBatchDetail = { batch: AdvertisingCandidateBatch; items: AdvertisingSubmission[] };
export type AdvertisingActivity = { batches: AdvertisingCandidateBatch[]; items: AdvertisingSubmission[] };
export type AdvertisingReviewRequest = {
    clientOperationId: string;
    expectedProductionRevision: number;
    decision: "approve" | "reject";
    reason: string;
} & (
    | { checkpoint: "candidate"; submissionId: string; clearSelected?: boolean; assemblyId?: never; renderAttemptId?: never; resourceId?: never; acceptedReviewId?: never }
    | { checkpoint: "assembly"; assemblyId: string; renderAttemptId: string; resourceId: string; submissionId?: never; clearSelected?: never; acceptedReviewId?: never }
    | { checkpoint: "delivery"; decision: "approve"; assemblyId: string; renderAttemptId: string; resourceId: string; acceptedReviewId: string; submissionId?: never; clearSelected?: never }
);
export type AdvertisingReview = {
    id: string;
    productionId: string;
    clientOperationId: string;
    checkpoint: "candidate" | "assembly" | "delivery";
    acceptedReviewId?: string;
    submissionId?: string;
    assemblyId?: string;
    renderAttemptId?: string;
    resourceId?: string;
    sourceRevision: number;
    sourceProductionRevision: number;
    selectionHash?: string;
    decision: "approve" | "reject";
    reason: string;
    clearSelected: boolean;
    actorId: string;
    createdAt: string;
};
export type AdvertisingReviewResult = { review: AdvertisingReview; production: AdvertisingProductionDetail };
export type AdvertisingIssueRequest = { clientOperationId: string; atMs?: number; category: string; note: string } & (
    { submissionId: string; assemblyId?: never; renderAttemptId?: never } | { assemblyId: string; renderAttemptId: string; submissionId?: never }
);
export type AdvertisingIssueResolution = { expectedRevision: number; status: "open" | "resolved"; resolutionNote: string };
export type AdvertisingIssue = {
    id: string;
    productionId: string;
    clientOperationId: string;
    submissionId?: string;
    assemblyId?: string;
    renderAttemptId?: string;
    atMs?: number;
    category: string;
    note: string;
    actorId: string;
    revision: number;
    status: "open" | "resolved";
    resolutionNote: string;
    resolvedBy?: string;
    createdAt: string;
    updatedAt: string;
};
export type AdvertisingEvent = {
    id: string;
    projectId: string;
    productionId?: string;
    shotId?: string;
    kind: string;
    actorId: string;
    sourceRevision: number;
    productionRevision: number;
    submissionId?: string;
    assemblyId?: string;
    renderAttemptId?: string;
    createdAt: string;
    fact: boolean;
    details: { taskId?: string; reviewId?: string; issueId?: string; decision?: string; status?: string; category?: string; note?: string; atMs?: number; selection?: AdvertisingSelection };
};
const base = (id: string) => "/ad-projects/" + encodeURIComponent(id) + "/productions";
const path = (id: string, p: string) => base(id) + "/" + encodeURIComponent(p);
const assemblyPath = (id: string, p: string, a: string) => path(id, p) + "/assemblies/" + encodeURIComponent(a);
export const createAdvertisingReview = (id: string, p: string, request: AdvertisingReviewRequest) => http.post<AdvertisingReviewResult>(path(id, p) + "/reviews", request);
export const getAdvertisingReviewForOperation = (id: string, p: string, clientOperationId: string, signal?: AbortSignal) => http.get<AdvertisingReviewResult>(path(id, p) + "/reviews", { params: compactApiParams({ clientOperationId }), signal });
export const listAdvertisingReviews = (id: string, p: string, page = 1, pageSize = 20, signal?: AbortSignal) => http.get<AdvertisingPage<AdvertisingReview>>(path(id, p) + "/reviews", { params: compactApiParams({ page, pageSize }), signal });
export const createAdvertisingIssue = (id: string, p: string, request: AdvertisingIssueRequest) => http.post<AdvertisingIssue>(path(id, p) + "/issues", request);
export const resolveAdvertisingIssue = (id: string, p: string, issue: string, request: AdvertisingIssueResolution) => http.patch<AdvertisingIssue>(path(id, p) + "/issues/" + encodeURIComponent(issue), request);
export const getAdvertisingIssueForOperation = (id: string, p: string, clientOperationId: string, signal?: AbortSignal) => http.get<AdvertisingIssue>(path(id, p) + "/issues", { params: compactApiParams({ clientOperationId }), signal });
export const listAdvertisingIssues = (id: string, p: string, page = 1, pageSize = 20, signal?: AbortSignal) => http.get<AdvertisingPage<AdvertisingIssue>>(path(id, p) + "/issues", { params: compactApiParams({ page, pageSize }), signal });
export const listAdvertisingEvents = (id: string, filters: { productionId?: string; shotId?: string; kind?: string; page?: number; pageSize?: number } = {}, signal?: AbortSignal) =>
    http.get<AdvertisingPage<AdvertisingEvent>>("/ad-projects/" + encodeURIComponent(id) + "/events", { params: compactApiParams(filters), signal });
export const prepareAdvertisingBatch = (id: string, p: string, request: AdvertisingBatchRequest) => http.post<AdvertisingBatchDetail>(path(id, p) + "/batches", request);
export const getAdvertisingBatchForOperation = (id: string, p: string, clientOperationId: string, signal?: AbortSignal) => http.get<AdvertisingBatchDetail>(path(id, p) + "/batches", { params: compactApiParams({ clientOperationId }), signal });
export const getAdvertisingBatch = (id: string, p: string, b: string, signal?: AbortSignal) => http.get<AdvertisingBatchDetail>(path(id, p) + "/batches/" + encodeURIComponent(b), { signal });
export const listAdvertisingBatches = (id: string, p: string, page = 1, pageSize = 20, signal?: AbortSignal) => http.get<AdvertisingPage<AdvertisingBatchDetail>>(path(id, p) + "/batches", { params: compactApiParams({ page, pageSize }), signal });
export const executeAdvertisingBatch = (id: string, p: string, b: string, items: AdvertisingBatchApproval[]) => http.post<AdvertisingBatchDetail>(path(id, p) + "/batches/" + encodeURIComponent(b) + "/execute", { items });
export const closeAdvertisingBatch = (id: string, p: string, b: string) => http.post<AdvertisingBatchDetail>(path(id, p) + "/batches/" + encodeURIComponent(b) + "/close", {});
export const getAdvertisingActivity = (id: string, p: string, signal?: AbortSignal) => http.get<AdvertisingActivity>(path(id, p) + "/activity", { signal });
export const createAdvertisingProduction = (id: string, confirmedRevisionId: string) => http.post<AdvertisingProductionDetail>(base(id), { confirmedRevisionId });
export const getAdvertisingProduction = (id: string, p: string, signal?: AbortSignal) => http.get<AdvertisingProductionDetail>(path(id, p), { signal });
export const saveAdvertisingProduction = (id: string, p: string, expectedRevision: number, settings: AdvertisingProductionSettings) => http.patch<AdvertisingProductionDetail>(path(id, p), { expectedRevision, settings });
export const prepareAdvertisingSubmission = (id: string, p: string, request: AdvertisingSubmissionRequest) => http.post<AdvertisingSubmission>(path(id, p) + "/submissions", request);
export const executeAdvertisingSubmission = (id: string, p: string, s: string, quoteHash: string) => http.post<AdvertisingSubmission>(path(id, p) + "/submissions/" + encodeURIComponent(s) + "/execute", { quoteHash });
export const listAdvertisingSubmissions = (id: string, p: string, page = 1, pageSize = 20, signal?: AbortSignal) => http.get<AdvertisingPage<AdvertisingSubmission>>(path(id, p) + "/submissions", { params: compactApiParams({ page, pageSize }), signal });
export const createAdvertisingAssembly = (id: string, p: string, expectedRevision: number, clientOperationId: string, fit: "pad" | "crop") => http.post<AdvertisingAssembly>(path(id, p) + "/assemblies", { expectedRevision, clientOperationId, fit });
export const getAdvertisingAssembly = (id: string, p: string, a: string, signal?: AbortSignal) => http.get<AdvertisingAssembly>(assemblyPath(id, p, a), { signal });
export const listAdvertisingAssemblies = (id: string, p: string, page = 1, pageSize = 20, signal?: AbortSignal) => http.get<AdvertisingPage<AdvertisingAssembly>>(path(id, p) + "/assemblies", { params: compactApiParams({ page, pageSize }), signal });
export const createAdvertisingRender = (id: string, p: string, a: string, clientOperationId: string) => http.post<AdvertisingRender>(assemblyPath(id, p, a) + "/renders", { clientOperationId });
export const listAdvertisingRenders = (id: string, p: string, a: string, page = 1, pageSize = 20, signal?: AbortSignal) =>
    http.get<AdvertisingPage<AdvertisingRender>>(assemblyPath(id, p, a) + "/renders", { params: compactApiParams({ page, pageSize }), signal });
