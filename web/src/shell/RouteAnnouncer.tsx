import { useEffect, useRef, useState } from "react";
import { useLocation, useMatches, useParams } from "react-router";
import { isMessageKey, useT } from "@/i18n";
import type { RouteHandle } from "@/registry/types";
import { patternOf } from "./boot";
import { useRegionalName } from "./regional";
import { setCurrentScreen } from "./report";

/** On every route change: the document title ("<title> · <regional>"), focus to the `<h1>`
 *  (or `#content`), a live-region announcement, and the reporter's current screen pattern.
 *  The first render sets the title but leaves focus alone. */
export function RouteAnnouncer() {
  const t = useT();
  const regional = useRegionalName();
  const location = useLocation();
  const matches = useMatches();
  const params = useParams();
  const [announcement, setAnnouncement] = useState("");

  const last = matches[matches.length - 1];
  const handle = last?.handle as RouteHandle | undefined;
  const titleKey = handle?.title;
  const title = titleKey && isMessageKey(titleKey) ? t(titleKey) : "";
  const pathname = location.pathname;

  useEffect(() => {
    setCurrentScreen(patternOf(pathname, params));
  }, [pathname, params]);

  useEffect(() => {
    document.title = title ? `${title} · ${regional}` : regional;
  }, [title, regional]);

  const titleRef = useRef(title);
  useEffect(() => {
    titleRef.current = title;
  }, [title]);
  // Only a change of path moves focus: a filter that rewrites the query string must leave the
  // person on the control they are using (WCAG 3.2.2).
  const prevPath = useRef<string | null>(null);
  useEffect(() => {
    if (prevPath.current === null || prevPath.current === pathname) {
      prevPath.current = pathname; // first render: no focus move
      return;
    }
    prevPath.current = pathname;
    const target =
      document.querySelector<HTMLElement>("main h1") ?? document.getElementById("content");
    if (target) {
      if (!target.hasAttribute("tabindex")) target.setAttribute("tabindex", "-1");
      target.focus();
    }
    setAnnouncement(titleRef.current);
  }, [pathname]);

  return (
    <div role="status" aria-live="polite" className="sr-only">
      {announcement}
    </div>
  );
}
