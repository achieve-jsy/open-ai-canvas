import { compactApiParams, http } from "./request";
import type { AdvertisingPage, AdvertisingRevisionDetail } from "./advertising-project";
import type { GenerationTask } from "./task-center";
import type { CreationSubmission } from "./creation-runs";
import type { TimelineProject } from "@/types/timeline";

export type AdvertisingSelection = { videoSubmissionId: string; audioSubmissionId: string; videoStartMs: number };
export type AdvertisingProductionSettings = { ttsModelId: string; voiceId: string; selections: Record<string, AdvertisingSelection>; musicResourceId: string; musicVolume: number };
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
    requestHash: string;
    publicRequest: { prompt: string; modelId: string; voiceId?: string; format?: string; speed?: number; durationSeconds?: number; size?: string; resolution?: string; referenceResourceIds?: string[] };
    quote: CreationSubmission["quote"];
    task?: GenerationTask;
};
export type AdvertisingAssembly = { id: string; productionId: string; clientOperationId: string; sourceProductionRevision: number; selectionHash: string; timeline: TimelineProject; canvasId?: string; createdAt: string; updatedAt: string };
export type AdvertisingRender = { id: string; assemblyId: string; clientOperationId: string; taskId: string; createdAt: string; task: GenerationTask; resourceId?: string; available: boolean };
const base = (id: string) => "/ad-projects/" + encodeURIComponent(id) + "/productions";
const path = (id: string, p: string) => base(id) + "/" + encodeURIComponent(p);
const assemblyPath = (id: string, p: string, a: string) => path(id, p) + "/assemblies/" + encodeURIComponent(a);
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
