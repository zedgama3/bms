import { useState } from "react";
import { useFormat, useT } from "@/i18n";
import { controlClass, Field } from "@/ui";
import type { FieldMessage } from "@/ui";
import { browserZone, instantToLocal, localToInstant, supportedZones } from "./helpers";
import { text } from "./text";
import type { EventKind, EventRecord } from "./types";

/** What an empty date sends: Go's zero time, which the server answers with `required` on the
 *  input instead of a parse error that belongs to no input. */
const ZERO_INSTANT = "0001-01-01T00:00:00Z";

export interface EventValues {
  name: string;
  kind: string;
  dates: { start: string; end: string };
  zone: string;
  copyFrom: string;
}

/** Reads the event form: the `datetime-local` values are wall-clock time in the chosen zone. */
export function readEvent(data: FormData): EventValues {
  const zone = text(data.get("zone")).trim();
  const at = (name: string) => localToInstant(text(data.get(name)), zone) || ZERO_INSTANT;
  return {
    name: text(data.get("name")).trim(),
    kind: text(data.get("kind")),
    dates: { start: at("dates.start"), end: at("dates.end") },
    zone,
    copyFrom: text(data.get("copy_from")),
  };
}

/** The inputs of the new-event and edit-event forms. `name`s equal the API's field paths, so a
 *  field error lands on its input. With `events` given (the new form) it offers "Copy set-up
 *  from", preselecting the newest event of the chosen kind. */
export function EventFields({
  event,
  kinds,
  kindLabel,
  events,
  errors,
}: {
  event?: EventRecord;
  kinds: readonly EventKind[];
  kindLabel: (key: string) => string;
  events?: readonly EventRecord[];
  errors: Readonly<Record<string, FieldMessage>>;
}) {
  const t = useT();
  const format = useFormat();
  const zone = event?.zone ?? browserZone();
  const [kind, setKind] = useState(event?.kind ?? kinds[0]?.key ?? "");
  const [copy, setCopy] = useState<string | null>(null);
  const zones = supportedZones();
  const zoneList = zones.includes(zone) ? zones : [zone, ...zones];
  // The events arrive newest first, so the first of the kind is the newest.
  const suggested = events?.find((e) => e.kind === kind)?.id ?? "";
  const copyValue = copy ?? suggested;

  return (
    <>
      <Field
        label={t("core.events.field.name")}
        name="name"
        required
        maxLength={100}
        autoComplete="off"
        defaultValue={event?.name ?? ""}
        error={errors["name"]}
      />
      <Field label={t("core.events.field.kind")} name="kind" required error={errors["kind"]}>
        {(control) => (
          <select
            {...control}
            className={controlClass}
            value={kind}
            onChange={(e) => {
              setKind(e.target.value);
              setCopy(null);
            }}
          >
            {kinds.map((k) => (
              <option key={k.key} value={k.key}>
                {kindLabel(k.key)}
              </option>
            ))}
            {kinds.some((k) => k.key === kind) ? null : <option value={kind}>{kind}</option>}
          </select>
        )}
      </Field>
      <Field
        label={t("core.events.field.zone")}
        name="zone"
        required
        hint={t("core.events.field.zone_hint")}
        error={errors["zone"]}
        {...(zones.length === 0
          ? { defaultValue: zone, autoComplete: "off" as const, maxLength: 64 }
          : {})}
      >
        {zones.length === 0
          ? undefined
          : (control) => (
              <select {...control} className={controlClass} defaultValue={zone}>
                {zoneList.map((z) => (
                  <option key={z} value={z}>
                    {z}
                  </option>
                ))}
              </select>
            )}
      </Field>
      <Field
        label={t("core.events.field.start")}
        name="dates.start"
        type="datetime-local"
        required
        autoComplete="off"
        defaultValue={event ? instantToLocal(event.dates.start, event.zone) : ""}
        error={errors["dates.start"]}
      />
      <Field
        label={t("core.events.field.end")}
        name="dates.end"
        type="datetime-local"
        required
        autoComplete="off"
        defaultValue={event ? instantToLocal(event.dates.end, event.zone) : ""}
        error={errors["dates.end"]}
      />
      {events ? (
        <Field
          label={t("core.events.field.copy_from")}
          name="copy_from"
          hint={t("core.events.field.copy_from_hint")}
          error={errors["copy_from"]}
        >
          {(control) => (
            <select
              {...control}
              className={controlClass}
              value={copyValue}
              onChange={(e) => setCopy(e.target.value)}
            >
              <option value="">{t("core.events.field.copy_none")}</option>
              {events.map((e) => (
                <option key={e.id} value={e.id}>
                  {t("core.events.field.copy_option", {
                    name: e.name,
                    date: format.date(e.dates.start, e.zone),
                  })}
                </option>
              ))}
            </select>
          )}
        </Field>
      ) : null}
    </>
  );
}
