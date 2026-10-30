import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useLocation, useNavigate } from "react-router";
import { ApiError, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import {
  Button,
  controlClass,
  EmptyState,
  ErrorSummary,
  Field,
  useFieldErrors,
  useToast,
} from "@/ui";
import { vol } from "./calls";
import { omitEmpty, text } from "./helpers";
import { useEvents, usePositions } from "./queries";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { carry } from "./back";
import type { BackState } from "./back";
import { ScreenError, useConflictMessage } from "./ScreenError";
import { browserZone, fromLocalInput, toLocalInput, zoneNames } from "./time";
import type { ShiftInput, ShiftSummary } from "./types";
import { useAction } from "./useAction";

/** The shift form (new, or edit with `shift`): position, event or standing work with a zone,
 *  label, start and end in that zone, slots, location, notes. */
export function ShiftFormBody({ department, shift }: { department: string; shift?: ShiftSummary }) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const act = useAction();
  const errors = useFieldErrors(act.error);
  const conflict = useConflictMessage(act.error);
  const positions = usePositions(department);
  const events = useEvents();
  const [event, setEvent] = useState(shift?.event ?? "");
  const [zone, setZone] = useState(shift?.zone ?? browserZone());

  const chosen = (events.data ?? []).find((e) => e.id === event);
  const effectiveZone = chosen ? chosen.zone : zone;
  const open = (events.data ?? []).filter((e) => !e.closed || e.id === shift?.event);

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const v = (n: string) => text(data.get(n)).trim();
    void act.run(async () => {
      // Required fields left empty are left out, so the service answers `required` on them.
      const body: Partial<ShiftInput> = {
        ...omitEmpty({
          position: v("position"),
          label: v("label"),
          starts: v("starts") ? fromLocalInput(v("starts"), effectiveZone) : "",
          ends: v("ends") ? fromLocalInput(v("ends"), effectiveZone) : "",
        }),
        ...(v("slots") === "" ? {} : { slots: Number(v("slots")) }),
        ...(event ? { event } : { zone }),
        location: v("location"),
        notes: v("notes"),
      };
      const saved = shift
        ? await vol.updateShift(shift.id, body)
        : await vol.createShift(department, body);
      await client.invalidateQueries({ queryKey: ["volunteers"] });
      toast.show(t(shift ? "module.volunteers.form.saved" : "module.volunteers.form.created"));
      void navigate(`/volunteers/shifts/${saved.id}`, {
        replace: true,
        state: shift
          ? carry(location)
          : ({
              back: { to: `/volunteers/departments/${department}?tab=shifts`, kind: "department" },
            } satisfies BackState),
      });
    });
  };

  // The form needs both lists; a failed read says so, with a retry, instead of an empty form.
  const loadError =
    positions.error instanceof ApiError
      ? { error: positions.error, retry: () => void positions.refetch() }
      : events.error instanceof ApiError
        ? { error: events.error, retry: () => void events.refetch() }
        : null;
  if (loadError) return <ScreenError error={loadError.error} onRetry={loadError.retry} />;

  if (positions.data && positions.data.length === 0)
    return (
      <EmptyState
        action={
          <Link
            to={`/volunteers/departments/${department}?tab=positions`}
            className="inline-flex min-h-11 items-center underline"
          >
            {t("module.volunteers.form.go_positions")}
          </Link>
        }
      >
        {t("module.volunteers.form.no_positions")}
      </EmptyState>
    );

  return (
    <form ref={formRef} onSubmit={submit} noValidate className="flex flex-col gap-5">
      <ErrorSummary
        error={act.error}
        formRef={formRef}
        {...(conflict ? { message: conflict } : {})}
      />
      <Field
        label={t("module.volunteers.label.position")}
        name="position"
        required
        error={errors["position"]}
      >
        {(control) => (
          <select {...control} className={controlClass} defaultValue={shift?.position ?? ""}>
            <option value="" disabled>
              {t("module.volunteers.choose")}
            </option>
            {(positions.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        )}
      </Field>
      <Field label={t("module.volunteers.label.event")} name="event" error={errors["event"]}>
        {(control) => (
          <select
            {...control}
            className={controlClass}
            value={event}
            onChange={(e) => setEvent(e.target.value)}
          >
            <option value="">{t("module.volunteers.label.standing")}</option>
            {open.map((ev) => (
              <option key={ev.id} value={ev.id}>
                {ev.name}
              </option>
            ))}
          </select>
        )}
      </Field>
      {event ? (
        <p className="text-sm text-muted">
          {t("module.volunteers.label.zone_event", { zone: effectiveZone })}
        </p>
      ) : (
        <Field
          label={t("module.volunteers.label.zone")}
          name="zone"
          hint={t("module.volunteers.label.zone_hint")}
          required
          error={errors["zone"]}
        >
          {(control) => (
            <select
              {...control}
              className={controlClass}
              value={zone}
              onChange={(e) => setZone(e.target.value)}
            >
              {zoneNames().map((z) => (
                <option key={z} value={z}>
                  {z}
                </option>
              ))}
            </select>
          )}
        </Field>
      )}
      <Field
        label={t("module.volunteers.label.label")}
        name="label"
        required
        maxLength={100}
        autoComplete="off"
        defaultValue={shift?.label ?? ""}
        error={errors["label"]}
      />
      <Field
        label={t("module.volunteers.label.starts")}
        name="starts"
        type="datetime-local"
        required
        autoComplete="off"
        defaultValue={shift ? toLocalInput(shift.starts, effectiveZone) : ""}
        error={errors["starts"]}
      />
      <Field
        label={t("module.volunteers.label.ends")}
        name="ends"
        type="datetime-local"
        required
        autoComplete="off"
        defaultValue={shift ? toLocalInput(shift.ends, effectiveZone) : ""}
        error={errors["ends"]}
      />
      <Field
        label={t("module.volunteers.label.slots")}
        name="slots"
        type="number"
        inputMode="numeric"
        required
        autoComplete="off"
        defaultValue={shift ? String(shift.slots) : "1"}
        error={errors["slots"]}
      />
      <Field
        label={t("module.volunteers.label.location")}
        name="location"
        maxLength={200}
        autoComplete="off"
        defaultValue={shift?.location ?? ""}
        error={errors["location"]}
      />
      <Field
        label={t("module.volunteers.label.notes")}
        name="notes"
        hint={t("module.volunteers.label.notes_hint")}
        error={errors["notes"]}
      >
        {(control) => (
          <textarea
            {...control}
            className={controlClass}
            rows={4}
            maxLength={2000}
            autoComplete="off"
            defaultValue={shift?.notes ?? ""}
          />
        )}
      </Field>
      <ReadOnlyNote id="shiftform-readonly" readOnly={readOnly} />
      <div className="flex flex-wrap gap-3">
        <Button
          type="submit"
          busy={act.busy}
          disabled={readOnly}
          {...(readOnly ? { "aria-describedby": "shiftform-readonly" } : {})}
        >
          {t(shift ? "module.volunteers.form.save" : "module.volunteers.form.create")}
        </Button>
        <Link
          to={shift ? `/volunteers/shifts/${shift.id}` : `/volunteers/departments/${department}`}
          {...(shift ? { state: carry(location) } : {})}
          className="inline-flex min-h-11 items-center underline"
        >
          {t("module.volunteers.cancel")}
        </Link>
      </div>
    </form>
  );
}
