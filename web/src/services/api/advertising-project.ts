import { compactApiParams, http } from "./request";
import type { Project } from "./projects";

export type AdvertisingBrief = { productName: string; description: string; audience: string; sellingPoints: string[]; cta: string; durationMs: number };
export type AdvertisingBrand = { name: string; primaryColor: string; logoResourceId: string };
export type AdvertisingShot = { id: string; durationMs: number; visual: string; voiceover: string; onScreenText: string; referenceResourceIds: string[] };
export type AdvertisingDraft = { schemaVersion: 1; brief: AdvertisingBrief; brand: AdvertisingBrand; productResourceIds: string[]; additionalResourceIds: string[]; shots: AdvertisingShot[] };
export type AdvertisingResource = { id: string; title: string; mimeType: string; width: number; height: number; durationMs: number; available: boolean };
export type AdvertisingDetail = { project: Project; revision: number; draft: AdvertisingDraft; latestConfirmedRevisionId?: string; resources: AdvertisingResource[] };
export type AdvertisingSnapshot = { name: string; draft: AdvertisingDraft; resources: AdvertisingResource[] };
export type AdvertisingRevisionSummary = { id: string; sourceRevision: number; createdAt: string };
export type AdvertisingRevisionDetail = AdvertisingRevisionSummary & { projectId: string; snapshot: AdvertisingSnapshot };
export type AdvertisingConfirmation = { snapshot: AdvertisingRevisionDetail; currentRevision: number };
export type AdvertisingListItem = { id: string; name: string; productName: string; durationMs: number; revision: number; latestConfirmedRevisionId?: string; updatedAt: string };
export type AdvertisingPage<T> = { items: T[]; page: number; pageSize: number; total: number };
export type AdvertisingSaveRequest = { name: string; draft: AdvertisingDraft; expectedRevision: number };
const base = (id: string) => `/ad-projects/` + encodeURIComponent(id);
export const listAdvertisingProjects = (page = 1, pageSize = 20, signal?: AbortSignal) => http.get<AdvertisingPage<AdvertisingListItem>>("/ad-projects", { params: compactApiParams({ page, pageSize }), signal });
export const createAdvertisingProject = (name: string) => http.post<AdvertisingDetail>("/ad-projects", { name });
export const getAdvertisingProject = (id: string, signal?: AbortSignal) => http.get<AdvertisingDetail>(base(id), { signal });
export const saveAdvertisingProject = (id: string, request: AdvertisingSaveRequest) => http.put<AdvertisingDetail>(base(id), request);
export const confirmAdvertisingProject = (id: string, expectedRevision: number) => http.post<AdvertisingConfirmation>(base(id) + "/confirm", { expectedRevision });
export const listAdvertisingRevisions = (id: string, page = 1, pageSize = 20, signal?: AbortSignal) => http.get<AdvertisingPage<AdvertisingRevisionSummary>>(base(id) + "/revisions", { params: compactApiParams({ page, pageSize }), signal });
export const getAdvertisingRevision = (id: string, revisionId: string, signal?: AbortSignal) => http.get<AdvertisingRevisionDetail>(base(id) + "/revisions/" + encodeURIComponent(revisionId), { signal });
export const deleteAdvertisingProject = (id: string, expectedRevision: number) => http.delete<{ deleted: boolean }>(base(id), { params: compactApiParams({ expectedRevision }) });
export function emptyAdvertisingDraft(): AdvertisingDraft {
    return {
        schemaVersion: 1,
        brief: { productName: "", description: "", audience: "", sellingPoints: [], cta: "", durationMs: 15000 },
        brand: { name: "", primaryColor: "", logoResourceId: "" },
        productResourceIds: [],
        additionalResourceIds: [],
        shots: [],
    };
}
