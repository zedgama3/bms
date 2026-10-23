import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, useReadOnly } from "@/api";
import { useT } from "@/i18n";
import { Check, Loading, useToast } from "@/ui";
import { vol } from "../calls";
import { ScreenError } from "../ScreenError";

/** Whether other volunteers see my playa name on my shifts (`GET|PUT /me/listing`). Leads
 *  always see the roster, and the note says so. */
export function ListingSwitch() {
  const t = useT();
  const toast = useToast();
  const client = useQueryClient();
  const { readOnly } = useReadOnly();
  const q = useQuery({
    queryKey: ["volunteers", "listing"],
    queryFn: ({ signal }) => vol.listed({ signal }),
  });
  const save = useMutation({
    mutationFn: (listed: boolean) => vol.setListed(listed),
    onSuccess: async () => {
      toast.show(t("module.volunteers.me.listing_saved"));
      await client.invalidateQueries({ queryKey: ["volunteers", "listing"] });
    },
  });
  const error =
    q.error instanceof ApiError ? q.error : save.error instanceof ApiError ? save.error : null;

  return (
    <section aria-labelledby="me-listing" className="flex flex-col gap-3">
      <h2 id="me-listing" className="text-xl font-bold">
        {t("module.volunteers.me.privacy")}
      </h2>
      {q.isPending ? <Loading /> : null}
      {error ? (
        <ScreenError error={error} {...(q.error ? { onRetry: () => void q.refetch() } : {})} />
      ) : null}
      {q.data !== undefined ? (
        <Check
          checked={q.data}
          disabled={readOnly || save.isPending}
          onChange={(e) => save.mutate(e.target.checked)}
          label={t("module.volunteers.me.listing")}
          hint={readOnly ? t("module.volunteers.readonly") : t("module.volunteers.me.listing_hint")}
        />
      ) : null}
    </section>
  );
}
