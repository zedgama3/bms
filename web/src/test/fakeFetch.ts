import { vi } from "vitest";
import type { Mock } from "vitest";

export type Handler = (req: Request) => Response | Promise<Response>;

/** Installs a fake global fetch for one test; the mock records every Request it saw. */
export function installFetch(handler: Handler): Mock<(req: Request) => Promise<Response>> {
  const mock = vi.fn(async (req: Request) => handler(req));
  vi.stubGlobal("fetch", mock);
  return mock;
}

export function json(body: unknown, init: ResponseInit = {}): Response {
  const headers = new Headers(init.headers);
  if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  return new Response(JSON.stringify(body), { ...init, headers });
}

export function problem(
  status: number,
  body: Record<string, unknown>,
  headers: Record<string, string> = {},
): Response {
  return new Response(JSON.stringify({ status, ...body }), {
    status,
    headers: { "Content-Type": "application/problem+json", ...headers },
  });
}

export function noContent(headers: Record<string, string> = {}): Response {
  return new Response(null, { status: 204, headers });
}
