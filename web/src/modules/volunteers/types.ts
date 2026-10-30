// The wire shapes of /api/volunteers/, from the generated schema. The generated response
// types mark every field optional (an `internal` field may be absent, D46); the fields below
// are the ones the server always sends (IDs, names, times), narrowed so the screens read them
// without a guard. Everything else stays optional: an absent count or flag is zero, an
// absent list is empty.
import type { components } from "@/api";

type S = components["schemas"];
type Req<T, K extends keyof T> = T & Required<Pick<T, K>>;

export type ID = string;

export type Qualification = Req<S["VolunteersQualification"], "id" | "name">;
export type QualificationInput = S["VolunteersQualificationInput"];
export type QualRecord = Req<
  S["VolunteersRecord"],
  "id" | "qualification" | "person" | "acquired_on"
>;
export type Position = Req<S["VolunteersPosition"], "id" | "department" | "name" | "access">;
export type PositionInput = S["VolunteersPositionInput"];
export type MemberView = Req<S["VolunteersMemberView"], "id" | "department" | "person">;
export type Member = Req<S["VolunteersMember"], "id" | "department" | "person">;
export type Shift = Req<
  S["VolunteersShift"],
  "id" | "department" | "position" | "label" | "starts" | "ends" | "zone" | "slots"
>;
export type ShiftInput = S["VolunteersShiftInput"];
export type Eligibility = S["VolunteersEligibility"];
export type ShiftMates = S["VolunteersShiftMates"];
export type ShiftSummary = Req<
  S["VolunteersShiftSummary"],
  "id" | "department" | "position" | "label" | "starts" | "ends" | "zone" | "slots"
>;
export type Signup = Req<S["VolunteersSignup"], "id" | "shift" | "person">;
export type RosterEntry = Req<S["VolunteersRosterEntry"], "id" | "shift" | "person">;
export type Roster = { shift: Shift; entries?: RosterEntry[] };
export type ScheduleEntry = Omit<S["VolunteersScheduleEntry"], "shift" | "id"> & {
  /** The sign-up's ID. */
  id: ID;
  shift: Shift;
};

export interface ShiftQuery {
  department?: ID;
  event?: ID;
  position?: ID;
  /** Starts at or after this instant. */
  from?: string;
  /** Starts before this instant. */
  to?: string;
  canceled?: boolean;
  /** Only the shifts the person can take or already holds (V4.3). */
  eligible?: boolean;
}

/** The rows one page of a long list asks for; **Show more** appends the next (V1.2). */
export const PAGE_LIMIT = 100;

export interface Department {
  id: ID;
  name: string;
  archived: boolean;
}

export interface EventRef {
  id: ID;
  name: string;
  zone: string;
  closed: boolean;
  dates: { start: string; end: string };
}
