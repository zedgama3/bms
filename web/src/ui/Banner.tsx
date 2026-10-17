import { Info, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

export type BannerTone = "info" | "warn";

const tones: Record<BannerTone, string> = {
  info: "bg-info-bg text-info-fg border-info-fg",
  warn: "bg-warn-bg text-warn-fg border-warn-fg",
};

/** A status line under the header (`role="status"`). The icon and the text carry the meaning, not the colour. */
export function Banner({
  tone = "info",
  children,
  actions,
  live = true,
}: {
  tone?: BannerTone;
  children: ReactNode;
  actions?: ReactNode;
  /** False when an always-mounted `role="status"` container already hosts the banner. */
  live?: boolean;
}) {
  const Icon = tone === "warn" ? TriangleAlert : Info;
  return (
    <div
      {...(live ? { role: "status" } : {})}
      className={`flex flex-wrap items-center gap-3 border-b px-4 py-3 ${tones[tone]}`}
    >
      <Icon aria-hidden="true" className="size-5 shrink-0" />
      <p className="min-w-0 flex-1 basis-60">{children}</p>
      {actions}
    </div>
  );
}
