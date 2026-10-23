/** The module's permission names (module-volunteers.md "Exported"). The session lists what the
 *  person holds; the screens only hide what it does not show. The server decides every call. */
export const P = {
  signUp: "volunteers.shifts.signup",
  shiftsManage: "volunteers.shifts.manage",
  rostersRead: "volunteers.rosters.read",
  positionsManage: "volunteers.positions.manage",
  membersManage: "volunteers.members.manage",
  qualificationsManage: "volunteers.qualifications.manage",
  qualificationsRead: "volunteers.qualifications.read",
  qualificationsVerify: "volunteers.qualifications.verify",
} as const;

/** The six lead permissions that open the admin entry (department scope). */
export const LEAD_PERMISSIONS: readonly string[] = [
  P.shiftsManage,
  P.positionsManage,
  P.membersManage,
  P.qualificationsManage,
  P.qualificationsVerify,
  P.rostersRead,
];
