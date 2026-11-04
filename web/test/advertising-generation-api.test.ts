import { expect, test } from "bun:test";
import { apiClient } from "../src/services/api/request";
import * as api from "../src/services/api/advertising-generation";

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
        const settings = { ttsModelId: "", voiceId: "", selections: {}, musicResourceId: "", musicVolume: 0 };
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
