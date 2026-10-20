import { useEffect, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { ResetComplete } from "../ResetComplete";
import { ResetRequest } from "../ResetRequest";

/** What the reset page leaves in the history entry once it has taken the token out of the
 *  address: a flag, never the token. */
interface FlowState {
  resetFlow?: boolean;
}

/** `/accounts/password-reset` (public): the request form, or, when the address carries
 *  `?token=` (the link in the e-mail), the new-password form. The token is held in memory only
 *  and is taken out of the address bar as soon as it has been read (identity.md "UI"); the
 *  history entry keeps a bare flag so the page stays on the new-password form. */
export function PasswordReset() {
  const [params] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const fromUrl = params.get("token");
  const [held, setHeld] = useState<string | null>(fromUrl);
  if (fromUrl !== null && fromUrl !== held) setHeld(fromUrl);

  useEffect(() => {
    if (fromUrl === null) return;
    void navigate({ pathname: location.pathname }, { replace: true, state: { resetFlow: true } });
  }, [fromUrl, location.pathname, navigate]);

  const inFlow = fromUrl !== null || (location.state as FlowState | null)?.resetFlow === true;
  return inFlow ? <ResetComplete token={held ?? ""} /> : <ResetRequest />;
}
