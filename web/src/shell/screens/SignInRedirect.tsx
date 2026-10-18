import { Navigate, useLocation } from "react-router";

/** `/sign-in` since M1: the sign-in screen is `/accounts/sign-in` (core/accounts); the query
 *  (`returnTo`) is kept so old links and bookmarks still work. */
export function SignInRedirect() {
  const { search } = useLocation();
  return <Navigate to={`/accounts/sign-in${search}`} replace />;
}
