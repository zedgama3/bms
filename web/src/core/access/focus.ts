/** Moves focus to a live element after the one that opened a dialog is gone (a revoked card, a
 *  deleted rule), so a keyboard or screen-reader user is not dropped at the top of the page. */
export function focusSoon(selector: string): void {
  setTimeout(() => {
    const el = document.querySelector<HTMLElement>(selector);
    if (!el) return;
    if (!el.hasAttribute("tabindex")) el.setAttribute("tabindex", "-1");
    el.focus();
  }, 0);
}
