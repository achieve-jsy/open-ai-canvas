import { expect, test, mock } from "bun:test";
import { spawnSync } from "node:child_process";

test("generic media upload sends decoded image size and rejects undecodable images", async () => {
    if (!process.env.MEDIA_IMAGE_CHECK) {
        const child = spawnSync(process.execPath, ["test", import.meta.path], { env: { ...process.env, MEDIA_IMAGE_CHECK: "1" }, encoding: "utf8", timeout: 30000 });
        expect(child.stderr + child.stdout).not.toContain("(fail)");
        expect(child.status).toBe(0);
        return;
    }
    // Bun has no image decoder or IndexedDB; keep transport and upload implementation real.
    let invalid = false;
    Object.defineProperty(globalThis, "Image", {
        configurable: true,
        value: class {
            naturalWidth = 400;
            naturalHeight = 300;
            onload?: () => void;
            onerror?: () => void;
            set src(value: string) {
                if (value) queueMicrotask(() => (invalid ? this.onerror?.() : this.onload?.()));
            }
        },
    });
    mock.module("../src/services/resource-blob-cache", () => ({ primeResourceBlobCache: async () => {}, getCachedResourceBlob: async () => null }));
    const { apiClient } = await import("../src/services/api/request");
    const { uploadMediaFile } = await import("../src/services/file-storage");
    const uploads: FormData[] = [];
    apiClient.defaults.adapter = async (config) => {
        const form = config.data as FormData;
        uploads.push(form);
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: { resource: { id: "image", mimeType: "image/png", width: Number(form.get("width")), height: Number(form.get("height")) } } } };
    };
    const file = new File(["image bytes"], "cup.png", { type: "image/png" });
    const result = await uploadMediaFile(file, "advertising");
    expect([uploads[0].get("width"), uploads[0].get("height")]).toEqual(["400", "300"]);
    expect([result.width, result.height]).toEqual([400, 300]);
    invalid = true;
    await expect(uploadMediaFile(file)).rejects.toThrow("图片加载失败");
    expect(uploads).toHaveLength(1);
});
