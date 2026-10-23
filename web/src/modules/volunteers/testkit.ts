// Fixtures shared by the volunteers tests (invented names and IDs only).
import { mountApp } from "@/test/app";
import { json, problem } from "@/test/fakeFetch";
import type { Handler } from "@/test/fakeFetch";

export const ME = "7d2c2f0e-0000-4000-8000-000000000001";
export const OTHER = "7d2c2f0e-0000-4000-8000-000000000009";
export const DEPT = "5c1a0000-0000-4000-8000-0000000000d1";
export const EVENT = "5c1a0000-0000-4000-8000-0000000000e1";
export const POS = "5c1a0000-0000-4000-8000-0000000000b1";
export const QUAL = "5c1a0000-0000-4000-8000-0000000000a1";
export const QUAL2 = "5c1a0000-0000-4000-8000-0000000000a2";
export const SHIFT = "5c1a0000-0000-4000-8000-0000000000c1";
export const SIGNUP = "5c1a0000-0000-4000-8000-0000000000f1";
export const RECORD = "5c1a0000-0000-4000-8000-0000000000f2";

export const V = "/api/volunteers";
export type Routes = Record<string, Handler>;

export const shift = (over: Record<string, unknown> = {}) => ({
  id: SHIFT,
  department: DEPT,
  department_name: "Gate",
  position: POS,
  position_name: "Greeter",
  label: "Morning dirt",
  starts: "2099-07-01T15:00:00Z",
  ends: "2099-07-01T19:00:00Z",
  zone: "America/Denver",
  slots: 4,
  location: "Ranger HQ",
  ...over,
});

export const base = (over: Routes = {}): Routes => ({
  "GET /api/departments/": () =>
    json({
      departments: [{ id: DEPT, name: "Gate", description: "", archived: false }],
    }),
  "GET /api/events/": () =>
    json({
      events: [
        {
          id: EVENT,
          name: "Midsummer",
          zone: "America/Denver",
          closed: false,
          kind: "main",
          dates: { start: "2099-06-28T00:00:00Z", end: "2099-07-05T00:00:00Z" },
        },
      ],
    }),
  [`GET ${V}/qualifications`]: () =>
    json({
      items: [
        { id: QUAL, name: "First aid", validity_days: 365 },
        { id: QUAL2, department: DEPT, name: "Radio licence" },
      ],
    }),
  [`GET ${V}/departments/${DEPT}/positions`]: () =>
    json({
      items: [{ id: POS, department: DEPT, name: "Greeter", access: "open", requirements: [] }],
    }),
  "GET /api/people/names": () => json({ names: { [OTHER]: "Pat Dust", [ME]: "Dusty" } }),
  "GET /api/people/me": () => problem(404, { code: "not_found" }),
  ...over,
});

export function mount(
  opts: { path: string; perms?: string[]; routes?: Routes; readOnly?: () => boolean } = {
    path: "/volunteers",
  },
) {
  return mountApp({
    path: opts.path,
    modules: [{ name: "volunteers", enabled: true }],
    session: {
      person_id: ME,
      account_id: "0b6f1c3e-0000-4000-8000-000000000002",
      playa_name: "Dusty",
      superuser: false,
      password_change_required: false,
      expires_at: "2099-01-01T00:00:00Z",
      signup_open: true,
      permissions: opts.perms ?? ["volunteers.shifts.signup"],
    },
    routes: base(opts.routes),
    ...(opts.readOnly ? { readOnly: opts.readOnly } : {}),
  });
}

type Mock = { mock: { calls: [Request][] } };
export async function bodyOf(fetchMock: Mock, method: string, path: string) {
  const hit = fetchMock.mock.calls.find(
    ([r]) => r.method === method && new URL(r.url).pathname === path,
  );
  return hit ? ((await hit[0].clone().json()) as unknown) : undefined;
}
export const seen = (fetchMock: Mock, method: string, path: string) =>
  fetchMock.mock.calls.some(([r]) => r.method === method && new URL(r.url).pathname === path);
export const queryOf = (fetchMock: Mock, method: string, path: string) => {
  const hit = [...fetchMock.mock.calls]
    .reverse()
    .find(([r]) => r.method === method && new URL(r.url).pathname === path);
  return hit ? new URL(hit[0].url).searchParams : undefined;
};
