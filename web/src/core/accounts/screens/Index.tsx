import { Navigate } from "react-router";

/** `/accounts/` has no screen of its own: it is the person's account page. */
export function Index() {
  return <Navigate to="/accounts/password" replace />;
}
