import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useT } from "@/i18n";
import { Button } from "./Button";

/** Auto-dismiss after 8 s (the spec's toasts). A toast is never the only place an error is shown. */
export const TOAST_MS = 8000;

interface ToastApi {
  show(text: string): void;
}
const Ctx = createContext<ToastApi>({ show() {} });

export function useToast(): ToastApi {
  return useContext(Ctx);
}

interface Item {
  id: number;
  text: string;
}

/** Hosts the toasts: a `role="status"` region at the bottom of the screen, within thumb reach. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const t = useT();
  const [items, setItems] = useState<Item[]>([]);
  const show = useCallback((text: string) => {
    setItems((cur) => [...cur, { id: Date.now() + Math.random(), text }]);
  }, []);
  const dismiss = useCallback(
    (id: number) => setItems((cur) => cur.filter((i) => i.id !== id)),
    [],
  );
  const api = useMemo(() => ({ show }), [show]);
  return (
    <Ctx.Provider value={api}>
      {children}
      <div
        role="status"
        className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 p-3"
      >
        {items.map((i) => (
          <ToastItem key={i.id} item={i} dismissLabel={t("shell.dismiss")} onDismiss={dismiss} />
        ))}
      </div>
    </Ctx.Provider>
  );
}

function ToastItem({
  item,
  dismissLabel,
  onDismiss,
}: {
  item: Item;
  dismissLabel: string;
  onDismiss: (id: number) => void;
}) {
  useEffect(() => {
    const id = setTimeout(() => onDismiss(item.id), TOAST_MS);
    return () => clearTimeout(id);
  }, [item.id, onDismiss]);
  return (
    <div className="pointer-events-auto flex w-full max-w-md items-center gap-3 rounded-md border border-border bg-bg p-3 shadow-lg">
      <p className="min-w-0 flex-1">{item.text}</p>
      <Button variant="secondary" onClick={() => onDismiss(item.id)}>
        {dismissLabel}
      </Button>
    </div>
  );
}
