import type { components } from "@/api";

export type Role = components["schemas"]["AccessRole"];
export type RoleInput = components["schemas"]["AccessRoleInput"];
export type Grant = components["schemas"]["AccessGrant"];
export type Rule = components["schemas"]["AccessRule"];
export type PermissionInfo = components["schemas"]["AccessPermission"];
export type Match = components["schemas"]["PeopleMatchList"]["matches"][number];
