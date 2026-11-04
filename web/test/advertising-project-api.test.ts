import { expect, test } from "bun:test";
import { apiClient } from "../src/services/api/request";
import * as api from "../src/services/api/advertising-project";

test("advertising API preserves paths, revisions, cancellation and envelope failures", async () => {
    const original = apiClient.defaults.adapter;
    const calls: any[] = [];
    let fail = false;
    apiClient.defaults.adapter = async (config) => {
        calls.push(config);
        return { data: fail ? { code: 409, reason: "advertising_revision_conflict", msg: "conflict" } : { code: 0, data: { revision: 2 } }, status: 200, statusText: "OK", headers: {}, config };
    };
    try {
        const signal = new AbortController().signal;
        await api.listAdvertisingProjects(1, 20, signal);
        await api.createAdvertisingProject("Ad");
        await api.getAdvertisingProject("a/b", signal);
        const draft = api.emptyAdvertisingDraft();
        await api.saveAdvertisingProject("ad", { name: "Ad", draft, expectedRevision: 1 });
        await api.confirmAdvertisingProject("ad", 2);
        await api.listAdvertisingRevisions("ad", 1, 20, signal);
        await api.getAdvertisingRevision("ad", "v/1", signal);
        await api.deleteAdvertisingProject("ad", 3);
        expect(calls.map((c) => c.url)).toEqual(["/ad-projects", "/ad-projects", "/ad-projects/a%2Fb", "/ad-projects/ad", "/ad-projects/ad/confirm", "/ad-projects/ad/revisions", "/ad-projects/ad/revisions/v%2F1", "/ad-projects/ad"]);
        expect(JSON.parse(calls[3].data)).toEqual({ name: "Ad", draft, expectedRevision: 1 });
        expect(JSON.parse(calls[4].data)).toEqual({ expectedRevision: 2 });
        expect(calls[7].params).toEqual({ expectedRevision: 3 });
        expect(calls[0].signal).toBe(signal);
        fail = true;
        await expect(api.confirmAdvertisingProject("ad", 2)).rejects.toMatchObject({ reason: "advertising_revision_conflict" });
    } finally {
        apiClient.defaults.adapter = original;
    }
});
