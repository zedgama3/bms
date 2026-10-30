import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { ApiError, useReadOnly, useSession, searchPeople } from "@/api";
import { isMessageKey, useT } from "@/i18n";
import {
  Banner,
  Button,
  Check,
  EmptyState,
  ErrorSummary,
  linkButtonClass,
  Loading,
  Table,
  useToast,
  PersonSearch,
} from "@/ui";
import type { Column } from "@/ui";
import { vol } from "../calls";
import { ConfirmDialog } from "../ConfirmDialog";
import { holdsAt } from "../helpers";
import { Mates } from "../Mates";
import { P } from "../permissions";
import { nameIn, useDepartments, useQualifications } from "../queries";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError, useConflictMessage } from "../ScreenError";
import { ShiftWhen } from "../ShiftWhen";
import { SignUpControl } from "../SignUpControl";
import type { RosterEntry } from "../types";
import { useAction } from "../useAction";

/** `/volunteers/shifts/:shift`: one shift; sign up or cancel my booking; who else is on it
 *  (when the server shows that); for roster readers the roster, and for managers Assign, edit
 *  and cancel. */
export function ShiftPage() {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const { shift: id = "" } = useParams();
  const [dialog, setDialog] = useState<"mine" | "shift" | "entry" | null>(null);
  const [entry, setEntry] = useState<RosterEntry | null>(null);

  const q = useQuery({
    queryKey: ["volunteers", "shift", id],
    queryFn: ({ signal }) => vol.shift(id, { signal }),
  });
  const s = q.data;
  const dept = s?.department ?? "";
  const canRoster = holdsAt(session, P.rostersRead, dept);
  const canManage = holdsAt(session, P.shiftsManage, dept);

  const quals = useQualifications();
  const departments = useDepartments();
  const roster = useQuery({
    queryKey: ["volunteers", "roster", id],
    enabled: s !== undefined && canRoster,
    queryFn: ({ signal }) => vol.roster(id, { signal }),
  });

  const refresh = () => client.invalidateQueries({ queryKey: ["volunteers"] });
  const error = q.error instanceof ApiError ? q.error : null;
  const rosterError = roster.error instanceof ApiError ? roster.error : null;
  const entries = roster.data?.entries ?? [];
  const showLegal = entries.some((e) => e.legal_name !== undefined && e.legal_name !== "");
  const reasonKey = `module.volunteers.cancel_reason.${s?.cancel_reason ?? ""}`;

  const columns: Column<RosterEntry>[] = [
    {
      header: t("module.volunteers.roster.person"),
      rowHeader: true,
      cell: (e) => (
        <Link to={`/people/${e.person}`} className="inline-flex min-h-11 items-center underline">
          {e.playa_name || t("module.volunteers.roster.unnamed")}
        </Link>
      ),
    },
    ...(showLegal
      ? [
          {
            header: t("module.volunteers.roster.legal_name"),
            cell: (e: RosterEntry) => e.legal_name ?? "",
          },
        ]
      : []),
    {
      header: t("module.volunteers.roster.status"),
      cell: (e) => (
        <ul className="flex flex-col gap-1">
          <li>
            {e.eligible === true
              ? t("module.volunteers.roster.eligible")
              : t("module.volunteers.roster.not_eligible")}
          </li>
          {e.waived ? (
            <li className="font-semibold">{t("module.volunteers.roster.waived")}</li>
          ) : null}
        </ul>
      ),
    },
    ...(canManage
      ? [
          {
            header: t("module.volunteers.roster.actions"),
            cell: (e: RosterEntry) => (
              <Button
                variant="danger"
                disabled={readOnly}
                onClick={() => {
                  setEntry(e);
                  setDialog("entry");
                }}
              >
                {t("module.volunteers.roster.remove")}
                <span className="sr-only"> {e.playa_name}</span>
              </Button>
            ),
          },
        ]
      : []),
  ];

  return (
    <div className="flex flex-col gap-5" aria-busy={q.isPending || undefined}>
      <div>
        <Link to="/volunteers" className="inline-flex min-h-11 items-center underline">
          {t("module.volunteers.shift.back")}
        </Link>
      </div>
      {!s ? <h1 className="text-2xl font-bold">{t("module.volunteers.shift.title")}</h1> : null}
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {s ? (
        <>
          <h1 className="text-2xl font-bold">{s.label}</h1>
          {s.canceled_at ? (
            <Banner tone="warn">
              {t("module.volunteers.shift.canceled")} {isMessageKey(reasonKey) ? t(reasonKey) : ""}
            </Banner>
          ) : null}
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1">
            <dt className="font-semibold">{t("module.volunteers.shift.when")}</dt>
            <dd>
              <ShiftWhen shift={s} />
            </dd>
            <dt className="font-semibold">{t("module.volunteers.shift.where")}</dt>
            <dd className="min-w-0 break-words">
              {s.location || t("module.volunteers.shift.no_location")}
            </dd>
            <dt className="font-semibold">{t("module.volunteers.shift.department")}</dt>
            <dd>{s.department_name || nameIn(departments.data, s.department)}</dd>
            <dt className="font-semibold">{t("module.volunteers.shift.position")}</dt>
            <dd>{s.position_name ?? ""}</dd>
            <dt className="font-semibold">{t("module.volunteers.shift.slots")}</dt>
            <dd>
              {t("module.volunteers.shift.filled", { booked: s.booked ?? 0, slots: s.slots })}
            </dd>
          </dl>
          {s.notes ? <p className="whitespace-pre-line">{s.notes}</p> : null}

          <section aria-labelledby="shift-signup" className="flex flex-col gap-3">
            <h2 id="shift-signup" className="text-xl font-bold">
              {t("module.volunteers.shift.your_booking")}
            </h2>
            <SignUpControl shift={s} qualifications={quals.data} />
            {s.mine ? (
              <div>
                <Button
                  variant="danger"
                  disabled={readOnly}
                  onClick={() => setDialog("mine")}
                  {...(readOnly ? { "aria-describedby": "shift-readonly" } : {})}
                >
                  {t("module.volunteers.shift.cancel_mine")}
                </Button>
                <ReadOnlyNote id="shift-readonly" readOnly={readOnly} />
              </div>
            ) : null}
          </section>

          {s.mates ? <Mates mates={s.mates} /> : null}

          {canManage ? (
            <section aria-labelledby="shift-manage" className="flex flex-col gap-3">
              <h2 id="shift-manage" className="text-xl font-bold">
                {t("module.volunteers.shift.manage")}
              </h2>
              {!s.canceled_at ? (
                <AssignForm
                  shiftId={s.id}
                  onDone={() => {
                    toast.show(t("module.volunteers.assign.done"));
                    return refresh();
                  }}
                />
              ) : null}
              <div className="flex flex-wrap gap-3">
                {!s.canceled_at ? (
                  <Link
                    to={`/volunteers/shifts/${s.id}/edit`}
                    className={linkButtonClass("secondary")}
                  >
                    {t("module.volunteers.shift.edit")}
                  </Link>
                ) : null}
                {!s.canceled_at ? (
                  <Button
                    variant="danger"
                    disabled={readOnly}
                    onClick={() => setDialog("shift")}
                    {...(readOnly ? { "aria-describedby": "shift-readonly-manage" } : {})}
                  >
                    {t("module.volunteers.shift.cancel")}
                  </Button>
                ) : null}
              </div>
              <ReadOnlyNote id="shift-readonly-manage" readOnly={readOnly} />
            </section>
          ) : null}

          {canRoster ? (
            <section aria-labelledby="shift-roster" className="flex flex-col gap-3">
              <h2 id="shift-roster" className="text-xl font-bold">
                {t("module.volunteers.roster.title")}
              </h2>
              {roster.isPending ? <Loading /> : null}
              {rosterError ? (
                <ScreenError error={rosterError} onRetry={() => void roster.refetch()} />
              ) : null}
              {roster.data && entries.length === 0 ? (
                <EmptyState>{t("module.volunteers.roster.empty")}</EmptyState>
              ) : null}
              {entries.length > 0 ? (
                <Table
                  caption={t("module.volunteers.roster.caption", { count: entries.length })}
                  columns={columns}
                  rows={entries}
                  rowKey={(e) => e.id}
                />
              ) : null}
            </section>
          ) : null}

          <ConfirmDialog
            open={dialog === "mine"}
            title={t("module.volunteers.shift.cancel_mine_title")}
            body={t("module.volunteers.shift.cancel_mine_body", { label: s.label })}
            confirm={t("module.volunteers.shift.cancel_mine_confirm")}
            dismiss={t("module.volunteers.shift.keep_mine")}
            returnFocus="main h1"
            onClose={() => setDialog(null)}
            onConfirm={async () => {
              await vol.cancelSignup(s.mine ?? "");
              toast.show(t("module.volunteers.booking_canceled"));
              await refresh();
            }}
          />
          <ConfirmDialog
            open={dialog === "shift"}
            title={t("module.volunteers.shift.cancel_title")}
            body={t("module.volunteers.shift.cancel_body", { label: s.label })}
            confirm={t("module.volunteers.shift.cancel_confirm")}
            dismiss={t("module.volunteers.shift.keep")}
            returnFocus="main h1"
            onClose={() => setDialog(null)}
            onConfirm={async () => {
              await vol.cancelShift(s.id);
              toast.show(t("module.volunteers.shift.canceled_done"));
              await refresh();
            }}
          />
          <ConfirmDialog
            open={dialog === "entry"}
            title={t("module.volunteers.roster.remove_title")}
            body={t("module.volunteers.roster.remove_body", { name: entry?.playa_name ?? "" })}
            confirm={t("module.volunteers.roster.remove_confirm")}
            returnFocus="main h1"
            onClose={() => setDialog(null)}
            onConfirm={async () => {
              if (!entry) return;
              await vol.cancelSignup(entry.id);
              toast.show(t("module.volunteers.booking_canceled"));
              await refresh();
            }}
          />
        </>
      ) : null}
    </div>
  );
}

/** Assign someone to the shift: a playa-name search and a Waive requirements switch that says
 *  the waiver is recorded. The switch works for the viewer too (a lead may waive for themself,
 *  Q-VOL7). An `unnamed` conflict says the person has no playa name yet. */
function AssignForm({ shiftId, onDone }: { shiftId: string; onDone: () => Promise<void> }) {
  const t = useT();
  const { readOnly } = useReadOnly();
  const formRef = useRef<HTMLFormElement>(null);
  const [person, setPerson] = useState<{ id: string; name: string } | null>(null);
  const [waive, setWaive] = useState(false);
  const act = useAction();
  const conflict = useConflictMessage(act.error);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!person) return;
    void act
      .run(async () => {
        await vol.assign(shiftId, person.id, waive);
        await onDone();
      })
      .then((ok) => {
        if (ok) {
          setPerson(null);
          setWaive(false);
        }
      });
  };
  return (
    <form
      ref={formRef}
      onSubmit={submit}
      noValidate
      aria-labelledby="assign-title"
      className="flex flex-col gap-4 rounded-md border border-border p-3"
    >
      <h3 id="assign-title" className="text-lg font-bold">
        {t("module.volunteers.assign.title")}
      </h3>
      <ErrorSummary
        error={act.error}
        formRef={formRef}
        {...(conflict ? { message: conflict } : {})}
      />
      <PersonSearch
        search={searchPeople}
        name="person"
        label={t("module.volunteers.assign.person")}
        chosenName={person?.name ?? ""}
        hasValue={person !== null}
        onPick={(m) =>
          setPerson({ id: m.id, name: m.playa_name || t("module.volunteers.roster.unnamed") })
        }
        onClear={() => setPerson(null)}
      />
      <Check
        name="waive"
        checked={waive}
        onChange={(e) => setWaive(e.target.checked)}
        label={t("module.volunteers.assign.waive")}
        hint={t("module.volunteers.assign.waive_hint")}
      />
      <ReadOnlyNote id="assign-readonly" readOnly={readOnly} />
      <div>
        <Button
          type="submit"
          busy={act.busy}
          disabled={readOnly || person === null}
          {...(readOnly ? { "aria-describedby": "assign-readonly" } : {})}
        >
          {t("module.volunteers.assign.submit")}
        </Button>
      </div>
    </form>
  );
}
