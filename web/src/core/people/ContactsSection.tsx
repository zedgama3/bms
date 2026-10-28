import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useAction, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Button, Dialog, EmptyState, ErrorSummary, Field, useFieldErrors, useToast } from "@/ui";
import { focusSoon } from "./focus";
import { ReadOnlyNote } from "./ReadOnlyNote";
import { profileApi } from "./profileApi";
import type { Target } from "./profileApi";
import { text } from "./text";
import type { Contact } from "./types";

/** Most emergency contacts a person may have (people.md `MaxEmergencyContacts`). */
const MAX = 10;

type Editing = { kind: "add" } | { kind: "edit"; contact: Contact } | null;

/** Emergency contacts as cards; add and edit through a dialog, remove after a confirmation.
 *  Contacts are never required (people.md decision, Q-CORE7). */
export function ContactsSection({
  contacts,
  target,
  canEdit,
  onChanged,
}: {
  contacts: readonly Contact[];
  target: Target;
  canEdit: boolean;
  onChanged: () => void;
}) {
  const t = useT();
  const toast = useToast();
  const { readOnly } = useReadOnly();
  const [editing, setEditing] = useState<Editing>(null);
  const [removing, setRemoving] = useState<Contact | null>(null);
  const formRef = useRef<HTMLFormElement>(null);
  const removeRef = useRef<HTMLFormElement>(null);
  const save = useAction();
  const remove = useAction();
  const fieldErrors = useFieldErrors(save.error);
  const off = readOnly || !canEdit;
  const describedBy = readOnly ? { "aria-describedby": "contacts-readonly" } : {};

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!editing) return;
    const data = new FormData(e.currentTarget);
    const body = {
      name: text(data.get("name")).trim(),
      phone: text(data.get("phone")).trim(),
      relationship: text(data.get("relationship")).trim(),
    };
    void save.run(async () => {
      if (editing.kind === "edit") await profileApi.updateContact(target, editing.contact.id, body);
      else await profileApi.addContact(target, body);
      setEditing(null);
      toast.show(t("core.people.contacts.saved"));
      onChanged();
      focusSoon("#contacts-h");
    });
  };

  const confirmRemove = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const c = removing;
    if (!c) return;
    void remove.run(async () => {
      await profileApi.removeContact(target, c.id);
      setRemoving(null);
      toast.show(t("core.people.contacts.removed"));
      onChanged();
      focusSoon("#contacts-h");
    });
  };

  const current = editing?.kind === "edit" ? editing.contact : null;

  return (
    <section aria-labelledby="contacts-h" className="flex flex-col gap-3">
      <h2 id="contacts-h" className="text-xl font-bold">
        {t("core.people.contacts.title")}
      </h2>
      <p className="text-muted">{t("core.people.contacts.intro")}</p>
      {contacts.length === 0 ? <EmptyState>{t("core.people.contacts.empty")}</EmptyState> : null}
      {contacts.length > 0 ? (
        <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {contacts.map((c) => (
            <li key={c.id} className="rounded-md border border-border bg-surface p-4">
              <p className="break-words text-lg font-semibold">{c.name}</p>
              <p className="break-all">{c.phone}</p>
              {c.relationship ? <p className="text-muted">{c.relationship}</p> : null}
              {canEdit ? (
                <div className="mt-2 flex flex-wrap gap-3">
                  <Button
                    variant="secondary"
                    disabled={off}
                    {...describedBy}
                    onClick={() => {
                      save.clear();
                      setEditing({ kind: "edit", contact: c });
                    }}
                  >
                    {t("core.people.contacts.edit")}
                  </Button>
                  <Button
                    variant="danger"
                    disabled={off}
                    {...describedBy}
                    onClick={() => {
                      remove.clear();
                      setRemoving(c);
                    }}
                  >
                    {t("core.people.contacts.remove")}
                  </Button>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      {canEdit ? (
        contacts.length >= MAX ? (
          <p>{t("core.people.contacts.limit", { max: MAX })}</p>
        ) : (
          <div>
            <Button
              variant="secondary"
              disabled={off}
              {...describedBy}
              onClick={() => {
                save.clear();
                setEditing({ kind: "add" });
              }}
            >
              {t("core.people.contacts.add")}
            </Button>
          </div>
        )
      ) : null}
      <ReadOnlyNote id="contacts-readonly" readOnly={readOnly} />

      <Dialog
        open={editing !== null}
        title={current ? t("core.people.contacts.edit_title") : t("core.people.contacts.add_title")}
        onClose={() => setEditing(null)}
      >
        <form
          key={current?.id ?? "new"}
          ref={formRef}
          onSubmit={submit}
          className="flex flex-col gap-4"
          noValidate
        >
          <ErrorSummary error={save.error} formRef={formRef} />
          <Field
            label={t("core.people.contacts.name")}
            name="name"
            required
            autoComplete="off"
            defaultValue={current?.name ?? ""}
            error={fieldErrors["name"]}
          />
          <Field
            label={t("core.people.contacts.phone")}
            name="phone"
            type="tel"
            inputMode="tel"
            required
            maxLength={40}
            autoComplete="off"
            defaultValue={current?.phone ?? ""}
            error={fieldErrors["phone"]}
          />
          <Field
            label={t("core.people.contacts.relationship")}
            name="relationship"
            maxLength={60}
            autoComplete="off"
            defaultValue={current?.relationship ?? ""}
            error={fieldErrors["relationship"]}
          />
          <div className="flex flex-wrap gap-3">
            <Button type="submit" busy={save.busy}>
              {t("core.people.contacts.save")}
            </Button>
            <Button variant="secondary" onClick={() => setEditing(null)}>
              {t("core.people.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>

      <Dialog
        open={removing !== null}
        title={t("core.people.contacts.remove_title")}
        onClose={() => setRemoving(null)}
      >
        <form ref={removeRef} onSubmit={confirmRemove} className="flex flex-col gap-4">
          <ErrorSummary error={remove.error} formRef={removeRef} />
          <p>{t("core.people.contacts.remove_body", { name: removing?.name ?? "" })}</p>
          <div className="flex flex-wrap gap-3">
            <Button type="submit" variant="danger" busy={remove.busy}>
              {t("core.people.contacts.remove_confirm")}
            </Button>
            <Button variant="secondary" onClick={() => setRemoving(null)}>
              {t("core.people.cancel")}
            </Button>
          </div>
        </form>
      </Dialog>
    </section>
  );
}
