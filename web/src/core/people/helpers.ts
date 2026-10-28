export { holdsAny as holds } from "@/api";

/** A route the server put in data (a section item's href): a same-origin path or nothing. */
export function safeHref(href: string): string {
  if (!href.startsWith("/") || href.startsWith("//") || href.startsWith("/\\")) return "";
  // eslint-disable-next-line no-control-regex
  return /[\u0000-\u001f\u007f]/.test(href) ? "" : href;
}
