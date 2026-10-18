import { Navigate } from "react-router";

/** `/access/` has no screen of its own: roles are the front page. */
export function Index() {
  return <Navigate to="/access/roles" replace />;
}
