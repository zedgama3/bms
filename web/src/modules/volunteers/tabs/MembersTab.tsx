import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useReadOnly, useSession, searchPeople } from "@/api";
import { useT } from "@/i18n";
import { Button, EmptyState, ErrorSummary, Loading, useToast, PersonSearch } from "@/ui";
import { vol } from "../calls";
import { ConfirmDialog } from "../ConfirmDialog";
import { focusSoon, holdsAt } from "../helpers";
import { P } from "../permissions";
import { ShowMore, usePaged } from "../paging";
import { nameIn, usePositions } from "../queries";
import { ReadOnlyNote } from "../ReadOnlyNote";
import { ScreenError, useConflictMessage } from "../ScreenError";
import type { MemberView } from "../types";
import { useAction } from "../useAction";

/** The department's members with the positions each is eligible for, **Add** by playa-name
 *  search and **Remove** (`members.manage`; the list needs `rosters.read`). */
export function MembersTab({ department }: { department: string }) {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const session = useSession();
  const { readOnly } = useReadOnly();
  const canManage = holdsAt(session, P.membersManage, department);
  const formRef = useRef<HTMLFormElement>(null);
  const [person, setPerson] = useState<{ id: string; name: string } | null>(null);
  const [removing, setRemoving] = useState<MemberView | null>(null);
  const act = useAction();
  const conflict = useConflictMessage(act.error);
  const positions = usePositions(department);
  const {
    q,
    rows: list,
    error,
  } = usePaged<MemberView>(["volunteers", "members", department], (after, signal) =>
    vol.members(department, after, { signal }),
  );
  const refresh = () => client.invalidateQueries({ queryKey: ["volunteers"] });
  const nameOf = (m: MemberView) => m.playa_name || t("module.volunteers.roster.unnamed");

  const add = (e: FormEvent) => {
    e.preventDefault();
    if (!person) return;
    void act
      .run(async () => {
        await vol.addMember(department, person.id);
        toast.show(t("module.volunteers.tab.members.added"));
        await refresh();
      })
      .then((ok) => {
        if (ok) {
          setPerson(null);
          focusSoon("#members-add");
        }
      });
  };

  return (
    <div className="flex flex-col gap-4">
      {canManage ? (
        <form
          ref={formRef}
          onSubmit={add}
          noValidate
          aria-labelledby="members-add"
          className="flex flex-col gap-4 rounded-md border border-border p-3"
        >
          <h3 id="members-add" className="text-lg font-bold">
            {t("module.volunteers.tab.members.add")}
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
          <ReadOnlyNote id="members-readonly" readOnly={readOnly} />
          <div>
            <Button
              type="submit"
              busy={act.busy}
              disabled={readOnly || person === null}
              {...(readOnly ? { "aria-describedby": "members-readonly" } : {})}
            >
              {t("module.volunteers.tab.members.add_submit")}
            </Button>
          </div>
        </form>
      ) : null}
      {q.isPending ? <Loading /> : null}
      {error ? <ScreenError error={error} onRetry={() => void q.refetch()} /> : null}
      {q.data && list.length === 0 ? (
        <EmptyState>{t("module.volunteers.tab.members.empty")}</EmptyState>
      ) : null}
      {list.length > 0 ? (
        <ul className="flex flex-col gap-3">
          {list.map((m) => (
            <li
              key={m.id}
              className="flex flex-col gap-2 rounded-md border border-border bg-surface p-3"
            >
              <h3 className="text-lg font-bold">{nameOf(m)}</h3>
              <p className="text-muted">
                {(m.eligible ?? []).length === 0
                  ? t("module.volunteers.tab.members.eligible_none")
                  : t("module.volunteers.tab.members.eligible", {
                      names: (m.eligible ?? [])
                        .map(
                          (id) =>
                            nameIn(positions.data, id) || t("module.volunteers.why.a_position"),
                        )
                        .join(", "),
                    })}
              </p>
              {canManage ? (
                <div>
                  <Button
                    variant="danger"
                    disabled={readOnly}
                    {...(readOnly ? { "aria-describedby": "members-readonly" } : {})}
                    onClick={() => setRemoving(m)}
                  >
                    {t("module.volunteers.tab.members.remove")}
                    <span className="sr-only"> {nameOf(m)}</span>
                  </Button>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      <ShowMore q={q} />
      <ConfirmDialog
        open={removing !== null}
        title={t("module.volunteers.tab.members.remove_title")}
        body={t("module.volunteers.tab.members.remove_body", {
          name: removing ? nameOf(removing) : "",
        })}
        confirm={t("module.volunteers.tab.members.remove_confirm")}
        returnFocus="#tab-heading"
        onClose={() => setRemoving(null)}
        onConfirm={async () => {
          if (!removing) return;
          await vol.removeMember(department, removing.person);
          toast.show(t("module.volunteers.tab.members.removed"));
          await refresh();
        }}
      />
    </div>
  );
}
