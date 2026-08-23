import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

// versionApi memoises its request at module scope, so each test needs its
// own copy of the module rather than a shared one carrying the previous
// test's cached answer. Resetting the registry and re-importing is what
// keeps that memo untestable-by-accident from becoming untested.
//
// ApiError comes back from the same fresh registry on purpose: a reset
// rebuilds apiClient too, so a top-level `import { ApiError }` would be a
// different class object and every instanceof check would fail.
async function freshVersionApi() {
  vi.resetModules();
  const [{ getVersion }, { ApiError }] = await Promise.all([
    import("./versionApi"),
    import("./apiClient"),
  ]);
  return { getVersion, ApiError };
}

beforeEach(() => {
  vi.unstubAllGlobals();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("versionApi.getVersion", () => {
  it("reads the label from the public endpoint, unauthenticated", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { version: "v1.2.3" }));
    vi.stubGlobal("fetch", fetchMock);

    const { getVersion } = await freshVersionApi();

    await expect(getVersion()).resolves.toBe("v1.2.3");
    // No Authorization header, and no credentials: the route is public.
    expect(fetchMock).toHaveBeenCalledWith("/api/version");
  });

  it("returns the label verbatim rather than normalising it", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(200, { version: "2026.08-nightly" }));
    vi.stubGlobal("fetch", fetchMock);

    const { getVersion } = await freshVersionApi();

    // Not semver, no `v` prefix — the label is opaque and nothing here is
    // entitled to reshape it (ADR-0072).
    await expect(getVersion()).resolves.toBe("2026.08-nightly");
  });

  it("reports the uninjected default like any other label", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { version: "dev" }));
    vi.stubGlobal("fetch", fetchMock);

    const { getVersion } = await freshVersionApi();

    await expect(getVersion()).resolves.toBe("dev");
  });

  it("makes one request no matter how many callers ask", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { version: "v1.2.3" }));
    vi.stubGlobal("fetch", fetchMock);

    const { getVersion } = await freshVersionApi();

    await Promise.all([getVersion(), getVersion()]);
    await getVersion();

    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("rejects with an ApiError when the endpoint fails", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(500, { error: { code: "boom", message: "Nope." } }));
    vi.stubGlobal("fetch", fetchMock);

    const { getVersion, ApiError } = await freshVersionApi();

    await expect(getVersion()).rejects.toBeInstanceOf(ApiError);
  });

  it("does not retry a failure on a later call", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(500, {}));
    vi.stubGlobal("fetch", fetchMock);

    const { getVersion, ApiError } = await freshVersionApi();

    await expect(getVersion()).rejects.toBeInstanceOf(ApiError);
    await expect(getVersion()).rejects.toBeInstanceOf(ApiError);

    // A failed lookup stays failed for the life of the page — decoration
    // does not get to generate traffic.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
