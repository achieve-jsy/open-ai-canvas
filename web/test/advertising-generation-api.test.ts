import { expect, test } from "bun:test";
import { apiClient } from "../src/services/api/request";
import * as api from "../src/services/api/advertising-generation";

test("review API keeps explicit output identity and recovery is GET only", async () => {
    const original = apiClient.defaults.adapter;
    const calls: any[] = [];
    let fail = false;
    apiClient.defaults.adapter = async (config) => {
        calls.push(config);
        return { data: fail ? { code: 409, reason: "advertising_production_conflict", msg: "stale" } : { code: 0, data: { review: { id: "r" } } }, status: 200, statusText: "OK", headers: {}, config };
    };
    try {
        const signal = new AbortController().signal;
        const review = { clientOperationId: "op", expectedProductionRevision: 2, checkpoint: "assembly" as const, assemblyId: "a", renderAttemptId: "render", resourceId: "output", decision: "approve" as const, reason: "watched" };
        await api.createAdvertisingReview("ad/x", "p/y", review);
        await api.getAdvertisingReviewForOperation("ad/x", "p/y", "op", signal);
        await api.listAdvertisingReviews("ad/x", "p/y", 2, 10, signal);
        await api.createAdvertisingIssue("ad/x", "p/y", { clientOperationId: "issue", submissionId: "s", atMs: 0, category: "product", note: "check" });
        await api.resolveAdvertisingIssue("ad/x", "p/y", "i/z", { expectedRevision: 1, status: "resolved", resolutionNote: "checked" });
        await api.getAdvertisingIssueForOperation("ad/x", "p/y", "issue", signal);
        await api.listAdvertisingIssues("ad/x", "p/y", 1, 20, signal);
        await api.listAdvertisingEvents("ad/x", { productionId: "p/y", shotId: "shot", kind: "candidate_reviewed", page: 2, pageSize: 10 }, signal);
        const p = "/ad-projects/ad%2Fx/productions/p%2Fy";
        expect(calls.map((c) => [c.method, c.url])).toEqual([
            ["post", p + "/reviews"],
            ["get", p + "/reviews"],
            ["get", p + "/reviews"],
            ["post", p + "/issues"],
            ["patch", p + "/issues/i%2Fz"],
            ["get", p + "/issues"],
            ["get", p + "/issues"],
            ["get", "/ad-projects/ad%2Fx/events"],
        ]);
        expect(JSON.parse(calls[0].data)).toEqual(review);
        expect(calls[1].params).toEqual({ clientOperationId: "op" });
        expect(calls[5].params).toEqual({ clientOperationId: "issue" });
        for (const i of [1, 2, 5, 6, 7]) expect(calls[i].signal).toBe(signal);
        expect(calls[7].params).toEqual({ productionId: "p/y", shotId: "shot", kind: "candidate_reviewed", page: 2, pageSize: 10 });
        expect(JSON.parse(calls[3].data).atMs).toBe(0);
        expect(JSON.parse(calls[4].data)).toEqual({ expectedRevision: 1, status: "resolved", resolutionNote: "checked" });
        fail = true;
        await expect(api.createAdvertisingReview("ad/x", "p/y", review)).rejects.toMatchObject({ reason: "advertising_production_conflict" });
        expect(calls).toHaveLength(9);
    } finally {
        apiClient.defaults.adapter = original;
    }
});

test("batch API preserves all member approvals and GET-only operation recovery", async () => {
    const original = apiClient.defaults.adapter;
    const calls: any[] = [];
    let fail = false;
    apiClient.defaults.adapter = async (config) => {
        calls.push(config);
        return { data: fail ? { code: 409, reason: "advertising_batch_active", msg: "active" } : { code: 0, data: { batch: { id: "b" }, items: [] } }, status: 200, statusText: "OK", headers: {}, config };
    };
    try {
        const signal = new AbortController().signal;
        const request = { clientOperationId: "operation", shotId: "shot", kind: "video" as const, modelId: "model", count: 2 as const, adjustment: "clear logo" };
        const items = [
            { submissionId: "one", quoteHash: "hash-one" },
            { submissionId: "two", quoteHash: "hash-two" },
        ];
        await api.prepareAdvertisingBatch("ad/x", "p/y", request);
        await api.getAdvertisingBatchForOperation("ad/x", "p/y", "op/key", signal);
        await api.getAdvertisingBatch("ad/x", "p/y", "b/z", signal);
        await api.listAdvertisingBatches("ad/x", "p/y", 2, 20, signal);
        await api.executeAdvertisingBatch("ad/x", "p/y", "b/z", items);
        await api.getAdvertisingActivity("ad/x", "p/y", signal);
        await api.closeAdvertisingBatch("ad/x", "p/y", "b/z");
        const p = "/ad-projects/ad%2Fx/productions/p%2Fy";
        expect(calls.map((c) => [c.method, c.url])).toEqual([
            ["post", p + "/batches"],
            ["get", p + "/batches"],
            ["get", p + "/batches/b%2Fz"],
            ["get", p + "/batches"],
            ["post", p + "/batches/b%2Fz/execute"],
            ["get", p + "/activity"],
            ["post", p + "/batches/b%2Fz/close"],
        ]);
        expect(JSON.parse(calls[0].data)).toEqual(request);
        expect(calls[1].params).toEqual({ clientOperationId: "op/key" });
        expect(calls[3].params).toEqual({ page: 2, pageSize: 20 });
        expect(JSON.parse(calls[4].data)).toEqual({ items });
        for (const i of [1, 2, 3, 5]) expect(calls[i].signal).toBe(signal);
        fail = true;
        await expect(api.executeAdvertisingBatch("ad", "p", "b", items)).rejects.toMatchObject({ reason: "advertising_batch_active" });
        expect(calls).toHaveLength(8);
    } finally {
        apiClient.defaults.adapter = original;
    }
});

test("production API preserves operation keys, revisions, cancellation and errors", async () => {
    const original = apiClient.defaults.adapter;
    const calls: any[] = [];
    let fail = false;
    apiClient.defaults.adapter = async (config) => {
        calls.push(config);
        return { data: fail ? { code: 409, reason: "advertising_quote_expired", msg: "expired" } : { code: 0, data: { id: "result" } }, status: 200, statusText: "OK", headers: {}, config };
    };
    try {
        const signal = new AbortController().signal;
        await api.createAdvertisingProduction("ad/x", "revision");
        await api.getAdvertisingProduction("ad/x", "p/y", signal);
        const settings = { fit: "pad" as const, ttsModelId: "", voiceId: "", selections: {}, musicResourceId: "", musicVolume: 0 };
        await api.saveAdvertisingProduction("ad/x", "p/y", 2, settings);
        const request = { clientOperationId: "op", shotId: "shot", kind: "video" as const, modelId: "model", voiceId: "", format: "" };
        await api.prepareAdvertisingSubmission("ad/x", "p/y", request);
        await api.executeAdvertisingSubmission("ad/x", "p/y", "s/z", "hash");
        await api.listAdvertisingSubmissions("ad/x", "p/y", 2, 20, signal);
        await api.createAdvertisingAssembly("ad/x", "p/y", 2, "op", "pad");
        await api.getAdvertisingAssembly("ad/x", "p/y", "a/z", signal);
        await api.listAdvertisingAssemblies("ad/x", "p/y", 2, 20, signal);
        await api.createAdvertisingRender("ad/x", "p/y", "a/z", "op");
        await api.listAdvertisingRenders("ad/x", "p/y", "a/z", 2, 20, signal);
        const base = "/ad-projects/ad%2Fx/productions";
        const p = base + "/p%2Fy";
        expect(calls.map((c) => [c.method, c.url])).toEqual([
            ["post", base],
            ["get", p],
            ["patch", p],
            ["post", p + "/submissions"],
            ["post", p + "/submissions/s%2Fz/execute"],
            ["get", p + "/submissions"],
            ["post", p + "/assemblies"],
            ["get", p + "/assemblies/a%2Fz"],
            ["get", p + "/assemblies"],
            ["post", p + "/assemblies/a%2Fz/renders"],
            ["get", p + "/assemblies/a%2Fz/renders"],
        ]);
        expect(JSON.parse(calls[2].data)).toEqual({ expectedRevision: 2, settings });
        expect(JSON.parse(calls[3].data)).toEqual(request);
        expect(JSON.parse(calls[4].data)).toEqual({ quoteHash: "hash" });
        expect(JSON.parse(calls[6].data)).toEqual({ expectedRevision: 2, clientOperationId: "op", fit: "pad" });
        expect(JSON.parse(calls[9].data)).toEqual({ clientOperationId: "op" });
        for (const i of [1, 5, 7, 8, 10]) expect(calls[i].signal).toBe(signal);
        expect(calls[10].params).toEqual({ page: 2, pageSize: 20 });
        fail = true;
        await expect(api.executeAdvertisingSubmission("ad", "p", "s", "hash")).rejects.toMatchObject({ reason: "advertising_quote_expired" });
    } finally {
        apiClient.defaults.adapter = original;
    }
});
