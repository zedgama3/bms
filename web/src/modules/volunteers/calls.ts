// Every call this module makes, in one place, through the shell's typed `api` object over the
// generated schema. A list arrives as `{items: [...]}` and, because `items` is an internal
// field with omitempty (D46), an empty list arrives as `{}`: `list()` reads it as [].
import { api, download, upload } from "@/api";
import type {
  Department,
  EventRef,
  ID,
  Member,
  MemberView,
  Position,
  PositionInput,
  QualRecord,
  Qualification,
  QualificationInput,
  Roster,
  ScheduleEntry,
  Shift,
  ShiftInput,
  ShiftQuery,
  ShiftSummary,
  Signup,
} from "./types";

/** One page of a long list: its rows and the opaque cursor of the next page ("" = the last). */
export interface Paged<T> {
  items: T[];
  next: string;
}
import { PAGE_LIMIT } from "./types";

const B = "/api/volunteers";
type Sig = { signal?: AbortSignal };
const sig = (s?: Sig) => (s?.signal ? { signal: s.signal } : {});

/** The `items` of a list answer; absent (an empty list, D46) is []. */
export function list<T>(body: { items?: T[] } | undefined): T[] {
  return body?.items ?? [];
}

/** One page of a paged list: the generated rows mark every field optional, so each caller names
 *  the narrowed row type (`types.ts`), as the unpaged lists do. */
function paged<T>(body: { items?: object[]; next?: string } | undefined): Paged<T> {
  return { items: (body?.items ?? []) as T[], next: body?.next ?? "" };
}

export const vol = {
  // ---- catalogue
  qualifications: async (q: { department?: ID; archived?: boolean } = {}, s?: Sig) =>
    list(
      await api.GET(`${B}/qualifications`, {
        ...sig(s),
        params: {
          query: {
            ...(q.department ? { department: q.department } : {}),
            ...(q.archived ? { archived: true } : {}),
          },
        },
      }),
    ) as Qualification[],
  createQualification: (body: QualificationInput) =>
    api.POST(`${B}/qualifications`, { body }) as Promise<Qualification>,
  updateQualification: (id: ID, body: QualificationInput) =>
    api.PUT(`${B}/qualifications/{qualification}`, {
      params: { path: { qualification: id } },
      body,
    }) as Promise<Qualification>,
  archiveQualification: async (id: ID) => {
    await api.POST(`${B}/qualifications/{qualification}/archive`, {
      params: { path: { qualification: id } },
    });
  },

  // ---- records
  /** A person's records. `selfUpload` is the module's `self_upload` setting, present only on
   *  the person's own list (V1.4); an absent flag is false. */
  recordsOf: async (person: ID, s?: Sig) => {
    const body = (await api.GET(`${B}/people/{person}/qualifications`, {
      ...sig(s),
      params: { path: { person } },
    })) as { items?: QualRecord[]; self_upload?: boolean } | undefined;
    return { items: body?.items ?? [], selfUpload: body?.self_upload === true };
  },
  recordQualification: (
    person: ID,
    body: { qualification?: ID; acquired_on?: string; expires_on?: string },
    noExpiry = false,
  ) =>
    api.POST(`${B}/people/{person}/qualifications`, {
      params: { path: { person } },
      body: { ...body, ...(noExpiry ? { no_expiry: true } : {}) } as {
        qualification: ID;
        acquired_on: string;
        expires_on?: string;
      },
    }) as Promise<QualRecord>,
  uploadCertificate: (file: File, fields: Record<string, string>) =>
    upload<QualRecord>(`${B}/me/qualifications`, { file, fields }),
  pending: async (department?: ID, s?: Sig) =>
    list(
      await api.GET(`${B}/qualification-records`, {
        ...sig(s),
        params: { query: department ? { department } : {} },
      }),
    ) as QualRecord[],
  verifyRecord: (
    id: ID,
    body: { acquired_on?: string; expires_on?: string; no_expiry?: boolean },
  ) =>
    api.POST(`${B}/qualification-records/{record}/verify`, {
      params: { path: { record: id } },
      body,
    }) as Promise<QualRecord>,
  rejectRecord: async (id: ID) => {
    await api.POST(`${B}/qualification-records/{record}/reject`, {
      params: { path: { record: id } },
    });
  },
  removeRecord: async (id: ID) => {
    await api.DELETE(`${B}/qualification-records/{record}`, {
      params: { path: { record: id } },
    });
  },
  /** A certificate's bytes, for the shell's `Attachments` row: fetched through `@/api`, never a
   *  raw link (a 404 or 503 would show problem JSON in a new tab). */
  certificate: (id: ID, signal: AbortSignal): Promise<Blob> =>
    download(`${B}/qualification-records/${encodeURIComponent(id)}/certificate`, { signal }),

  // ---- positions
  positions: async (department: ID, archived = false, s?: Sig) =>
    list(
      await api.GET(`${B}/departments/{department}/positions`, {
        ...sig(s),
        params: { path: { department }, query: archived ? { archived: true } : {} },
      }),
    ) as Position[],
  createPosition: (department: ID, body: PositionInput) =>
    api.POST(`${B}/departments/{department}/positions`, {
      params: { path: { department } },
      body,
    }) as Promise<Position>,
  updatePosition: (id: ID, body: PositionInput) =>
    api.PUT(`${B}/positions/{position}`, {
      params: { path: { position: id } },
      body,
    }) as Promise<Position>,
  archivePosition: async (id: ID) => {
    await api.POST(`${B}/positions/{position}/archive`, { params: { path: { position: id } } });
  },
  setRequirements: (id: ID, qualifications: ID[]) =>
    api.PUT(`${B}/positions/{position}/requirements`, {
      params: { path: { position: id } },
      body: { qualifications },
    }) as Promise<Position>,

  // ---- members
  members: async (department: ID, after = "", s?: Sig) =>
    paged<MemberView>(
      await api.GET(`${B}/departments/{department}/members`, {
        ...sig(s),
        params: {
          path: { department },
          query: { limit: PAGE_LIMIT, ...(after ? { after } : {}) },
        },
      }),
    ),
  addMember: (department: ID, person: ID) =>
    api.POST(`${B}/departments/{department}/members`, {
      params: { path: { department } },
      body: { person },
    }) as Promise<Member>,
  removeMember: async (department: ID, person: ID) => {
    await api.DELETE(`${B}/departments/{department}/members/{person}`, {
      params: { path: { department, person } },
    });
  },
  myDepartments: async (s?: Sig) => list(await api.GET(`${B}/me/departments`, sig(s))) as Member[],
  leave: async (department: ID) => {
    await api.DELETE(`${B}/me/departments/{department}`, { params: { path: { department } } });
  },

  // ---- shifts (paged: `limit`, `after`, `next`)
  shifts: async (q: ShiftQuery = {}, after = "", s?: Sig) =>
    paged<ShiftSummary>(
      await api.GET(`${B}/shifts`, {
        ...sig(s),
        params: {
          query: {
            ...(q.department ? { department: q.department } : {}),
            ...(q.event ? { event: q.event } : {}),
            ...(q.position ? { position: q.position } : {}),
            ...(q.from ? { from: q.from } : {}),
            ...(q.to ? { to: q.to } : {}),
            ...(q.canceled ? { canceled: true } : {}),
            ...(q.eligible ? { eligible: true } : {}),
            limit: PAGE_LIMIT,
            ...(after ? { after } : {}),
          },
        },
      }),
    ),
  shift: (id: ID, s?: Sig) =>
    api.GET(`${B}/shifts/{shift}`, {
      ...sig(s),
      params: { path: { shift: id } },
    }) as Promise<ShiftSummary>,
  // A required field the person left empty is omitted (never sent as ""), so the service answers
  // `required` on that field; hence the Partial body, cast once here.
  createShift: (department: ID, body: Partial<ShiftInput>) =>
    api.POST(`${B}/departments/{department}/shifts`, {
      params: { path: { department } },
      body: body as ShiftInput,
    }) as Promise<Shift>,
  updateShift: (id: ID, body: Partial<ShiftInput>) =>
    api.PUT(`${B}/shifts/{shift}`, {
      params: { path: { shift: id } },
      body: body as ShiftInput,
    }) as Promise<Shift>,
  cancelShift: async (id: ID) => {
    await api.POST(`${B}/shifts/{shift}/cancel`, { params: { path: { shift: id } } });
  },
  copyShifts: (department: ID, body: { from?: ID; to?: ID }) =>
    api.POST(`${B}/departments/{department}/shifts/copy`, {
      params: { path: { department } },
      body: body as { from: ID; to: ID },
    }),

  // ---- sign-ups
  signUp: (shift: ID) =>
    api.POST(`${B}/shifts/{shift}/signups`, { params: { path: { shift } } }) as Promise<Signup>,
  assign: (shift: ID, person: ID, waive: boolean) =>
    api.POST(`${B}/shifts/{shift}/assign`, {
      params: { path: { shift } },
      body: { person, waive },
    }) as Promise<Signup>,
  cancelSignup: async (id: ID) => {
    await api.DELETE(`${B}/signups/{signup}`, { params: { path: { signup: id } } });
  },
  roster: (shift: ID, s?: Sig) =>
    api.GET(`${B}/shifts/{shift}/roster`, {
      ...sig(s),
      params: { path: { shift } },
    }) as Promise<Roster>,
  /** Upcoming (soonest first, with bookings someone else canceled) or past (newest first). */
  schedule: async (past: boolean, after = "", s?: Sig) =>
    paged<ScheduleEntry>(
      await api.GET(`${B}/me/schedule`, {
        ...sig(s),
        params: {
          query: {
            ...(past ? { past: true } : {}),
            limit: PAGE_LIMIT,
            ...(after ? { after } : {}),
          },
        },
      }),
    ),
  /** True only when the server says so: an absent flag is false (D46). */
  listed: async (s?: Sig): Promise<boolean> =>
    (await api.GET(`${B}/me/listing`, sig(s)))?.listed === true,
  setListed: async (listed: boolean) => {
    await api.PUT(`${B}/me/listing`, { body: { listed } });
  },
};

// ---- core reads this module needs (departments, events)
export const core = {
  departments: async (s?: Sig): Promise<Department[]> =>
    (await api.GET("/api/departments/", sig(s))).departments ?? [],
  events: async (s?: Sig): Promise<EventRef[]> =>
    (await api.GET("/api/events/", sig(s))).events ?? [],
};
