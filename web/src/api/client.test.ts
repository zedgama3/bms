import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, PROBLEM_CODES } from "@/api";
import type { paths } from "@/api";
import { configureClient, makeApi, moduleOf, TIMEOUT_MS } from "@/api/internal";
import type { ClientHooks } from "@/api/internal";
import openapi from "../../openapi.json";
import { installFetch, json, noContent, problem } from "@/test/fakeFetch";

function hooks(): { [K in keyof ClientHooks]: ReturnType<typeof vi.fn> } {
  const h = { onUnauthenticated: vi.fn(), onReadOnly: vi.fn(), onFailure: vi.fn() };
  configureClient(h);
  return h;
}

async function failure(p: Promise<unknown>): Promise<ApiError> {
  try {
    await p;
  } catch (e) {
    expect(e).toBeInstanceOf(ApiError);
    return e as ApiError;
  }
  throw new Error("expected the call to reject");
}

beforeEach(() => {
  hooks();
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("a successful call", () => {
  it("resolves with the parsed body and sends traceparent, credentials and Accept", async () => {
    const f = installFetch(() => json({ modules: [{ name: "hello", enabled: true }] }));
    const body = await api.GET("/api/modules");
    expect(body.modules[0]?.name).toBe("hello");
    const req = f.mock.calls[0]![0];
    expect(req.url).toBe(`${location.origin}/api/modules`);
    expect(req.method).toBe("GET");
    expect(req.credentials).toBe("same-origin");
    expect(req.headers.get("Accept")).toBe("application/json, application/problem+json");
    expect(req.headers.get("traceparent")).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-01$/);
    expect(req.headers.get("Authorization")).toBeNull();
  });

  it("sends a fresh traceparent per call", async () => {
    const f = installFetch(() => json({ modules: [] }));
    await api.GET("/api/modules");
    await api.GET("/api/modules");
    const [a, b] = f.mock.calls.map((c) => c[0].headers.get("traceparent"));
    expect(a).not.toBe(b);
  });

  it("resolves undefined for 204", async () => {
    installFetch(() => noContent());
    const body = await api.POST("/api/client-errors", { body: { reports: [] } });
    expect(body).toBeUndefined();
  });

  it("sends a JSON body with Content-Type", async () => {
    const f = installFetch(() => noContent());
    await api.POST("/api/client-errors", {
      body: { reports: [{ kind: "crash", message: "m", occurred_at: "2026-10-17T00:00:00Z" }] },
    });
    const req = f.mock.calls[0]![0];
    expect(req.headers.get("Content-Type")).toBe("application/json");
    expect(await req.text()).toContain('"kind":"crash"');
  });
});

describe("route and module derivation", () => {
  it("derives the module from the path", () => {
    expect(moduleOf("/api/hello/greeting")).toBe("hello");
    expect(moduleOf("/api/modules")).toBe("core");
    expect(moduleOf("/api/admin/modules")).toBe("core");
    expect(moduleOf("/api/client-errors")).toBe("core");
  });

  it("reports the route pattern, never the URL, and encodes path params", async () => {
    type P = {
      "/api/things/{id}": {
        parameters: { query?: never; header?: never; path?: never; cookie?: never };
        get: {
          parameters: {
            query?: { page?: number };
            header?: never;
            path: { id: string };
            cookie?: never;
          };
          requestBody?: never;
          responses: {
            200: {
              headers: Record<string, unknown>;
              content: { "application/json": { ok: boolean } };
            };
          };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
      };
    };
    const things = makeApi<P>();
    const f = installFetch(() => problem(404, { code: "not_found" }));
    const e = await failure(
      things.GET("/api/things/{id}", { params: { path: { id: "a b/c" }, query: { page: 2 } } }),
    );
    expect(e.route).toBe("/api/things/{id}");
    expect(e.module).toBe("things");
    expect(e.method).toBe("GET");
    const url = f.mock.calls[0]![0].url;
    expect(url).toBe(`${location.origin}/api/things/a%20b%2Fc?page=2`);
    expect(e.message).not.toContain("a b");
  });
});

describe("failures", () => {
  it("F1: a network failure becomes ApiError network and is reported", async () => {
    const h = hooks();
    installFetch(() => Promise.reject(new TypeError("Failed to fetch")));
    const e = await failure(api.GET("/api/modules"));
    expect(e.code).toBe("network");
    expect(e.status).toBe(0);
    expect(e.traceId).toMatch(/^[0-9a-f]{32}$/);
    expect(h.onFailure).toHaveBeenCalledWith(e);
  });

  it("F2: the client times out at 35 s with fake timers", async () => {
    vi.useFakeTimers();
    const h = hooks();
    installFetch(
      (req) =>
        new Promise((_, reject) => {
          req.signal.addEventListener("abort", () =>
            reject(new DOMException("aborted", "AbortError")),
          );
        }),
    );
    const p = failure(api.GET("/api/modules"));
    await vi.advanceTimersByTimeAsync(TIMEOUT_MS - 1);
    expect(h.onFailure).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(2);
    const e = await p;
    expect(e.code).toBe("network");
    expect(e.detail).toBe("timeout");
    expect(h.onFailure).toHaveBeenCalledTimes(1);
  });

  it("the caller's abort rejects with the AbortError, not an ApiError, and is not reported", async () => {
    const h = hooks();
    installFetch(
      (req) =>
        new Promise((_, reject) => {
          req.signal.addEventListener("abort", () =>
            reject(new DOMException("aborted", "AbortError")),
          );
        }),
    );
    const ctl = new AbortController();
    const p = api.GET("/api/modules", { signal: ctl.signal });
    ctl.abort();
    await expect(p).rejects.toMatchObject({ name: "AbortError" });
    expect(h.onFailure).not.toHaveBeenCalled();
  });

  it("F3: the problem body maps every field", async () => {
    installFetch(() =>
      problem(
        400,
        {
          code: "invalid",
          ref: "1A2B-3C4D",
          fields: [
            { field: "email", code: "required", params: { min: 1 }, reason: "must not be empty" },
          ],
        },
        { "Retry-After": "7" },
      ),
    );
    const e = await failure(api.GET("/api/modules"));
    expect(e).toMatchObject({ status: 400, code: "invalid", ref: "1A2B-3C4D", retryAfter: 7 });
    expect(e.fields).toEqual([
      { field: "email", code: "required", params: { min: "1" }, reason: "must not be empty" },
    ]);
  });

  it("drops a reference that is not XXXX-XXXX", async () => {
    installFetch(() => problem(500, { code: "internal", ref: "<script>" }));
    expect((await failure(api.GET("/api/modules"))).ref).toBeUndefined();
  });

  it("F4: a non-problem error body is malformed_response and reported", async () => {
    const h = hooks();
    installFetch(
      () =>
        new Response("<html>Bad gateway</html>", {
          status: 502,
          headers: { "Content-Type": "text/html" },
        }),
    );
    const e = await failure(api.GET("/api/modules"));
    expect(e).toMatchObject({ code: "malformed_response", status: 502 });
    expect(h.onFailure).toHaveBeenCalledWith(e);
  });

  it("F5: an unparsable success body is malformed_response", async () => {
    installFetch(
      () =>
        new Response("{not json", { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    const e = await failure(api.GET("/api/modules"));
    expect(e).toMatchObject({ code: "malformed_response", status: 200 });
  });

  it("F6: 401 calls onUnauthenticated with the current path and is still thrown", async () => {
    const h = hooks();
    history.replaceState(null, "", "/hello?x=1");
    installFetch(() => problem(401, { code: "unauthenticated" }));
    const e = await failure(api.GET("/api/modules"));
    expect(e.code).toBe("unauthenticated");
    expect(h.onUnauthenticated).toHaveBeenCalledWith("/hello?x=1");
    history.replaceState(null, "", "/");
  });

  it("F6: 401 on the session endpoint and on sign-in does not redirect", async () => {
    const h = hooks();
    type S = {
      "/api/accounts/session": {
        parameters: { query?: never; header?: never; path?: never; cookie?: never };
        get: {
          parameters: { query?: never; header?: never; path?: never; cookie?: never };
          requestBody?: never;
          responses: {
            200: { headers: Record<string, unknown>; content: { "application/json": unknown } };
          };
        };
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
      };
    };
    installFetch(() => problem(401, { code: "unauthenticated" }));
    await failure(makeApi<S>().GET("/api/accounts/session"));
    expect(h.onUnauthenticated).not.toHaveBeenCalled();
  });

  it.each([
    [403, "cross_origin"],
    [403, "misdirected"],
    [405, "method_not_allowed"],
    [413, "payload_too_large"],
    [415, "unsupported_media_type"],
  ])("F7: %i %s is surfaced as that code and passed to onFailure", async (status, code) => {
    const h = hooks();
    installFetch(() => problem(status, { code }));
    const e = await failure(api.GET("/api/modules"));
    expect(e.code).toBe(code);
    expect(h.onFailure).toHaveBeenCalledWith(e);
  });

  it("F8: 429 carries retryAfter", async () => {
    installFetch(() => problem(429, { code: "rate_limited" }, { "Retry-After": "12" }));
    expect(await failure(api.GET("/api/modules"))).toMatchObject({
      code: "rate_limited",
      retryAfter: 12,
    });
  });

  it("F9/F12: X-Read-Only toggles the store both ways and read_only is thrown", async () => {
    const h = hooks();
    let readOnly = true;
    installFetch(() =>
      json({ modules: [] }, { headers: readOnly ? { "X-Read-Only": "true" } : {} }),
    );
    await api.GET("/api/modules");
    expect(h.onReadOnly).toHaveBeenLastCalledWith(true);
    readOnly = false;
    await api.GET("/api/modules");
    expect(h.onReadOnly).toHaveBeenLastCalledWith(false);
    installFetch(() => problem(503, { code: "read_only" }, { "X-Read-Only": "true" }));
    const e = await failure(api.GET("/api/modules"));
    expect(e.code).toBe("read_only");
    expect(h.onReadOnly).toHaveBeenLastCalledWith(true);
  });

  it("F10: internal keeps its ref and trace id", async () => {
    installFetch(() => problem(500, { code: "internal", ref: "AAAA-1111" }));
    const e = await failure(api.GET("/api/modules"));
    expect(e).toMatchObject({ code: "internal", ref: "AAAA-1111" });
    expect(e.traceId).toMatch(/^[0-9a-f]{32}$/);
  });

  it("a throwing hook never changes what the caller sees", async () => {
    configureClient({
      onUnauthenticated() {},
      onReadOnly() {
        throw new Error("boom");
      },
      onFailure() {
        throw new Error("boom");
      },
    });
    installFetch(() => problem(500, { code: "internal" }));
    expect((await failure(api.GET("/api/modules"))).code).toBe("internal");
  });
});

describe("the codes table", () => {
  it("is pinned against web/openapi.json", () => {
    const desc = openapi.components.schemas.Problem.properties.code.description;
    const named = [...desc.matchAll(/\b[a-z]+(?:_[a-z]+)*\b/g)].map((m) => m[0]);
    for (const code of PROBLEM_CODES)
      expect(named, `${code} in the Problem description`).toContain(code);
    // every snake_case word of the list inside parentheses is a code the type knows
    const inParens = [...desc.matchAll(/\(([^)]*)\)/g)].flatMap((m) =>
      m[1]!.split(",").map((s) => s.trim()),
    );
    for (const code of inParens) expect(PROBLEM_CODES as readonly string[], code).toContain(code);
  });

  it("types the shell's own paths from the generated schema", () => {
    const keys: (keyof paths)[] = [
      "/api/modules",
      "/api/admin/modules",
      "/api/client-errors",
      "/api/openapi.json",
    ];
    expect(Object.keys(openapi.paths)).toEqual(expect.arrayContaining(keys));
  });
});
