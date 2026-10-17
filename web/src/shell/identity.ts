// The identity endpoints the frame calls (identity.md, Proposed change 5; PENDING: the target
// spec is not written and M0 answers 404 on all three). They are typed here, privately,
// instead of being hand-added to web/openapi.json, which `bms openapi` generates. When
// identity ships them they appear in schema.d.ts and this file is deleted in favour of `api`.
import { makeApi } from "@/api/internal";

interface NoParams {
  query?: never;
  header?: never;
  path?: never;
  cookie?: never;
}
type Json<T> = { headers: { [name: string]: unknown }; content: { "application/json": T } };
type NoContent = { headers: { [name: string]: unknown }; content?: never };
type Other = {
  put?: never;
  delete?: never;
  options?: never;
  head?: never;
  patch?: never;
  trace?: never;
};

export interface SessionBody {
  signed_in: boolean;
  person_id?: string;
  playa_name?: string;
  superuser?: boolean;
  permissions?: string[];
}

interface IdentityPaths {
  "/api/accounts/session": {
    parameters: NoParams;
    get: { parameters: NoParams; requestBody?: never; responses: { 200: Json<SessionBody> } };
    post?: never;
  } & Other;
  "/api/accounts/sign-in": {
    parameters: NoParams;
    get?: never;
    post: {
      parameters: NoParams;
      requestBody: { content: { "application/json": { email: string; password: string } } };
      responses: { 204: NoContent };
    };
  } & Other;
  "/api/accounts/sign-out": {
    parameters: NoParams;
    get?: never;
    post: { parameters: NoParams; requestBody?: never; responses: { 204: NoContent } };
  } & Other;
}

export const identity = makeApi<IdentityPaths>();
