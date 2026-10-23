import { useT } from "@/i18n";
import type { ShiftMates } from "./types";

/** "Who else is on this shift": playa names as plain text (no links), then how many are not
 *  listed. Rendered only when the server sent `mates`; absent means no section and no hint. */
export function Mates({ mates }: { mates: ShiftMates }) {
  const t = useT();
  const names = mates.names ?? [];
  const unlisted = mates.unlisted ?? 0;
  return (
    <section aria-labelledby="shift-mates" className="flex flex-col gap-2">
      <h2 id="shift-mates" className="text-xl font-bold">
        {t("module.volunteers.mates.title")}
      </h2>
      {names.length === 0 && unlisted === 0 ? <p>{t("module.volunteers.mates.none")}</p> : null}
      {names.length > 0 ? (
        <ul className="list-disc ps-5">
          {names.map((n, i) => (
            <li key={`${i}:${n}`}>{n}</li>
          ))}
        </ul>
      ) : null}
      {unlisted > 0 ? (
        <p className="text-muted">
          {t(
            names.length > 0
              ? "module.volunteers.mates.and_unlisted"
              : "module.volunteers.mates.unlisted",
            {
              count: unlisted,
            },
          )}
        </p>
      ) : null}
    </section>
  );
}
