import { api } from "@/api";
import type { Consent, Contact, ContactInput, Profile, ProfileInput } from "./types";

/** Who a profile call is about: `"me"` (the person themself) or another person's ID. The two
 *  have separate routes (`/api/people/me/...` and `/api/people/{person}/...`) with the same
 *  shapes, so the screens that edit a profile do not care which one they have. */
export type Target = "me" | { person: string };

const p = (person: string) => ({ params: { path: { person } } });

export const profileApi = {
  update(target: Target, body: ProfileInput): Promise<Profile> {
    return target === "me"
      ? api.PUT("/api/people/me", { body })
      : api.PUT("/api/people/{person}", { ...p(target.person), body });
  },
  addContact(target: Target, body: ContactInput): Promise<Contact> {
    return target === "me"
      ? api.POST("/api/people/me/contacts", { body })
      : api.POST("/api/people/{person}/contacts", { ...p(target.person), body });
  },
  updateContact(target: Target, contact: string, body: ContactInput): Promise<Contact> {
    return target === "me"
      ? api.PUT("/api/people/me/contacts/{contact}", { params: { path: { contact } }, body })
      : api.PUT("/api/people/{person}/contacts/{contact}", {
          params: { path: { person: target.person, contact } },
          body,
        });
  },
  async removeContact(target: Target, contact: string): Promise<void> {
    if (target === "me")
      await api.DELETE("/api/people/me/contacts/{contact}", { params: { path: { contact } } });
    else
      await api.DELETE("/api/people/{person}/contacts/{contact}", {
        params: { path: { person: target.person, contact } },
      });
  },
  setConsent(target: Target, purpose: string, granted: boolean): Promise<Consent> {
    return target === "me"
      ? api.PUT("/api/people/me/consents/{purpose}", {
          params: { path: { purpose } },
          body: { granted },
        })
      : api.PUT("/api/people/{person}/consents/{purpose}", {
          params: { path: { person: target.person, purpose } },
          body: { granted },
        });
  },
};
