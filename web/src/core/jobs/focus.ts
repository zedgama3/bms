/** Moves focus to a live element after the one that was pressed is gone (a control that appears
 *  or disappears), so a keyboard or screen-reader user keeps their place. */
export function focusSoon(selector: string): void {
  setTimeout(() => {
    const el = document.querySelector<HTMLElement>(selector);
    if (!el) return;
    if (el.tabIndex < 0 && !el.hasAttribute("tabindex")) el.setAttribute("tabindex", "-1");
    el.focus();
  }, 0);
}

/** The selector for the control whose `name` is given (a full setting key has dots). */
export function byName(name: string): string {
  return `[name="${CSS.escape(name)}"]`;
}
