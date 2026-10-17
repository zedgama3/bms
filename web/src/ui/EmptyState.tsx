import type { ReactNode } from "react";

/** "There is nothing here", with an optional way forward. */
export function EmptyState({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-start gap-3 rounded-md border border-dashed border-border bg-surface p-4">
      <p>{children}</p>
      {action}
    </div>
  );
}
