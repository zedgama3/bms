import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  I18nProvider,
  isMessageKey,
  resetFormatWarningsForTest,
  resetMissingWarningsForTest,
  useRichT,
  makeFormat,
  useFormat,
  useLanguage,
  useT,
} from "@/i18n";
import { addCatalogue, configureLanguages } from "./internal";
import { getLanguage, setLanguage } from "./lang";
import { termsOf } from "./terms";
import type { MessageKey } from "@/i18n";

function T({ k, values }: { k: MessageKey; values?: Record<string, string | number> }) {
  const t = useT();
  return <p data-testid="t">{t(k, values)}</p>;
}

function mount(node: React.ReactNode) {
  return render(<I18nProvider>{node}</I18nProvider>);
}

const key = (k: string) => k as MessageKey;

beforeEach(() => {
  configureLanguages(["en", "es"], "en");
  addCatalogue("shell", "en", {
    "shell.home.title": "Home",
    "shell.home.welcome": "Welcome to {regional}",
    "shell.only.en": "Only English",
    "term.event": "event",
    "term.theme_camp": "theme camp",
    "x.term": "Join the {term_event} at the {term_theme_camp}",
    "x.plural": "{n, plural, =0 {none} one {# item} other {# items}}",
  });
});
afterEach(() => {
  document.documentElement.lang = "";
});

describe("t()", () => {
  it("interpolates values and plurals", () => {
    const { rerender } = mount(<T k={key("x.plural")} values={{ n: 1 }} />);
    expect(screen.getByTestId("t")).toHaveTextContent("1 item");
    rerender(
      <I18nProvider>
        <T k={key("x.plural")} values={{ n: 5 }} />
      </I18nProvider>,
    );
    expect(screen.getByTestId("t")).toHaveTextContent("5 items");
  });

  it("substitutes {term_*} from the catalogue's term.* entries", () => {
    mount(<T k={key("x.term")} />);
    expect(screen.getByTestId("t")).toHaveTextContent("Join the event at the theme camp");
  });

  it("a regional's different terms change every message that uses them", () => {
    addCatalogue("regional", "en", { "term.event": "gathering" });
    mount(<T k={key("x.term")} />);
    expect(screen.getByTestId("t")).toHaveTextContent("Join the gathering at the theme camp");
  });

  it("F23: a key missing in es falls back to en", () => {
    addCatalogue("shell", "es", { "shell.home.title": "Inicio" });
    setLanguage("es");
    mount(
      <>
        <T k={key("shell.home.title")} />
        <T k={key("shell.only.en")} />
      </>,
    );
    const [a, b] = screen.getAllByTestId("t");
    expect(a).toHaveTextContent("Inicio");
    expect(b).toHaveTextContent("Only English");
  });

  it("F23: warns once per key, in development, when a language lacks it", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    resetMissingWarningsForTest();
    setLanguage("es");
    mount(
      <>
        <T k={key("shell.only.en")} />
        <T k={key("shell.only.en")} />
      </>,
    );
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn.mock.calls[0]![0]).toContain("shell.only.en");
  });

  it("F24: a key missing in en too returns the key literal and never throws", () => {
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    mount(<T k={key("nope.missing")} />);
    expect(screen.getByTestId("t")).toHaveTextContent("nope.missing");
    expect(err).not.toHaveBeenCalled();
  });

  it("a broken ICU message returns something and never throws", () => {
    addCatalogue("bad", "en", { "bad.msg": "Hello {unterminated" });
    mount(<T k={key("bad.msg")} />);
    expect(screen.getByTestId("t").textContent).toBeTruthy();
  });

  it("switching the language re-renders", () => {
    addCatalogue("shell", "es", { "shell.home.title": "Inicio" });
    function Switch() {
      const { set } = useLanguage();
      return <button onClick={() => set("es")}>go</button>;
    }
    mount(
      <>
        <T k={key("shell.home.title")} />
        <Switch />
      </>,
    );
    expect(screen.getByTestId("t")).toHaveTextContent("Home");
    act(() => screen.getByText("go").click());
    expect(screen.getByTestId("t")).toHaveTextContent("Inicio");
  });
});

function R({ k }: { k: MessageKey }) {
  const rich = useRichT();
  return <p data-testid="r">{rich(k)}</p>;
}

describe("F24a: a message that cannot be formatted", () => {
  beforeEach(() => {
    resetFormatWarningsForTest();
    addCatalogue("f", "en", {
      "f.ok": "Sign up for a {term_event}",
      "f.bad": "Sign up for a {term_x}",
      "f.rich": "See <en>{term_x}</en>",
      "f.rich_ok": "See <en>{term_event}</en>",
    });
  });

  it("a missing argument falls back to the en text", () => {
    addCatalogue("f", "es", { "f.ok": "Inscribe un {term_nope}" });
    setLanguage("es");
    mount(<T k={key("f.ok")} />);
    expect(screen.getByTestId("t")).toHaveTextContent("Sign up for a event");
  });

  it("a message that cannot be formatted returns the key", () => {
    mount(<T k={key("f.bad")} />);
    expect(screen.getByTestId("t").textContent).toBe("f.bad");
  });

  it("a value containing braces is shown as given", () => {
    mount(<T k={key("shell.home.welcome")} values={{ regional: "{term_x}" }} />);
    expect(screen.getByTestId("t").textContent).toBe("Welcome to {term_x}");
  });

  it("no raw placeholder reaches the screen", () => {
    const { container } = mount(
      <>
        <T k={key("f.bad")} />
        <R k={key("f.rich")} />
      </>,
    );
    expect(container.textContent).not.toContain("{term_");
  });

  it("the dev warning is given once per key", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    mount(
      <>
        <T k={key("f.bad")} />
        <T k={key("f.bad")} />
      </>,
    );
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn.mock.calls[0]![0]).toContain("f.bad");
    warn.mockRestore();
  });

  it("both es and en lacking an argument shows the key, warning once", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    addCatalogue("f", "es", { "f.bad": "Inscribe un {term_y}" });
    setLanguage("es");
    mount(<T k={key("f.bad")} />);
    expect(screen.getByTestId("t").textContent).toBe("f.bad");
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn.mock.calls[0]![0]).toContain("the key");
    warn.mockRestore();
  });

  it("the same key warns again in a second language", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    configureLanguages(["en", "es", "fr"], "en");
    mount(<T k={key("f.bad")} />);
    act(() => setLanguage("es"));
    addCatalogue("f", "es", { "f.bad": "Inscribe un {term_y}" });
    act(() => setLanguage("fr"));
    addCatalogue("f", "fr", { "f.bad": "Inscrire {term_y}" });
    warn.mockClear();
    resetFormatWarningsForTest();
    const langs = ["en", "es", "en", "es"];
    for (const l of langs) {
      act(() => setLanguage(l));
    }
    expect(warn.mock.calls.length).toBe(2);
    warn.mockRestore();
  });

  it("a language with no Intl locale data still formats a plain message", () => {
    configureLanguages(["en", "zz"], "en");
    addCatalogue("f", "zz", { "f.plain": "Zed text" });
    addCatalogue("f", "en", { "f.plain": "Plain text" });
    setLanguage("zz");
    mount(<T k={key("f.plain")} />);
    expect(screen.getByTestId("t").textContent).toBe("Zed text");
  });

  it("useRichT: a missing argument falls back to the en text", () => {
    addCatalogue("f", "es", { "f.rich_ok": "Ver <en>{term_nope}</en>" });
    setLanguage("es");
    const { container } = mount(<R k={key("f.rich_ok")} />);
    expect(container.textContent).toBe("See event");
    expect(container.querySelector('span[lang="en"]')).not.toBeNull();
  });

  it("useRichT: a message that cannot be formatted returns the key, warned once", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    mount(
      <>
        <R k={key("f.rich")} />
        <R k={key("f.rich")} />
      </>,
    );
    for (const el of screen.getAllByTestId("r")) expect(el.textContent).toBe("f.rich");
    expect(warn).toHaveBeenCalledTimes(1);
    warn.mockRestore();
  });
});

describe("terms", () => {
  it("turns term.theme_camp into term_theme_camp", () => {
    expect(termsOf({ "term.event": "event", "term.theme_camp": "theme camp", other: "x" })).toEqual(
      {
        term_event: "event",
        term_theme_camp: "theme camp",
      },
    );
  });
});

describe("module terms", () => {
  it("reads module.<name>.term.<key>[.<form>] as term_<key>[_<form>]", () => {
    expect(
      termsOf({
        "module.volunteers.term.shift": "shift",
        "module.volunteers.term.shift.plural_title": "Shifts",
        "module.volunteers.setting.x.title": "not a term",
      }),
    ).toEqual({ term_shift: "shift", term_shift_plural_title: "Shifts" });
  });

  it("lets a term.<key> entry win over the module's default, whatever the order", () => {
    expect(
      termsOf({
        "term.shift": "stint",
        "module.volunteers.term.shift": "shift",
        "module.volunteers.term.shift.plural": "shifts",
      }),
    ).toEqual({ term_shift: "stint", term_shift_plural: "shifts" });
  });
});

describe("isMessageKey", () => {
  it("is true for keys of the loaded en catalogues and false otherwise", () => {
    expect(isMessageKey("shell.home.title")).toBe(true);
    expect(isMessageKey("module.other.banner.hi")).toBe(false);
    addCatalogue("other", "en", { "module.other.banner.hi": "Hi" });
    expect(isMessageKey("module.other.banner.hi")).toBe(true);
    expect(isMessageKey("toString")).toBe(false);
    expect(isMessageKey("")).toBe(false);
  });
});

describe("language choice (decision 7)", () => {
  it("defaults to the regional's default, ignoring the browser's language list", () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("es-MX");
    configureLanguages(["en", "es"], "en");
    expect(getLanguage()).toBe("en");
    expect(document.documentElement.lang).toBe("en");
  });

  it("persists an explicit choice in localStorage['bms.lang'], the only key", () => {
    setLanguage("es");
    expect(localStorage.getItem("bms.lang")).toBe("es");
    expect(localStorage.length).toBe(1);
    expect(document.documentElement.lang).toBe("es");
    configureLanguages(["en", "es"], "en");
    expect(getLanguage()).toBe("es");
  });

  it("F26: a stored tag that is not offered is ignored", () => {
    localStorage.setItem("bms.lang", "fr");
    configureLanguages(["en", "es"], "en");
    expect(getLanguage()).toBe("en");
  });

  it("an unoffered tag cannot be set", () => {
    setLanguage("fr");
    expect(getLanguage()).toBe("en");
    expect(localStorage.getItem("bms.lang")).toBeNull();
  });

  it("F25: storage errors are ignored; the choice lives in memory", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(() => configureLanguages(["en", "es"], "en")).not.toThrow();
    expect(() => setLanguage("es")).not.toThrow();
    expect(getLanguage()).toBe("es");
  });
});

describe("formatting", () => {
  it("formats dates in an event's zone and shows the zone when it differs from the browser's", () => {
    const f = makeFormat("en");
    const browser = new Intl.DateTimeFormat().resolvedOptions().timeZone;
    const other = browser === "Asia/Tokyo" ? "America/Los_Angeles" : "Asia/Tokyo";
    const instant = "2026-10-17T03:30:00Z";
    expect(f.time(instant, other)).toMatch(/[A-Z]{2,5}|GMT/);
    expect(f.time(instant, browser)).not.toMatch(/GMT|UTC|[A-Z]{3,}/);
    expect(f.date(instant, "Asia/Tokyo")).toBe("Oct 17, 2026");
    expect(f.date(instant, "America/Los_Angeles")).toBe("Oct 16, 2026");
    expect(f.dateTime(instant, "Asia/Tokyo")).toContain("12:30");
  });

  it("an invalid instant or zone never throws", () => {
    const f = makeFormat("en");
    expect(f.date("not a date")).toBe("");
    expect(() => f.time("2026-10-17T03:30:00Z", "Not/AZone")).not.toThrow();
  });

  it("formats numbers, lists and relative times in the language", () => {
    const en = makeFormat("en");
    const es = makeFormat("es");
    expect(en.number(1234.5)).toBe("1,234.5");
    expect(es.number(1234.5)).toMatch(/1\.?234,5/);
    expect(en.list(["a", "b", "c"])).toBe("a, b, and c");
    expect(es.list(["a", "b"])).toBe("a y b");
    expect(en.relative(new Date(Date.now() + 3 * 3600_000))).toBe("in 3 hours");
  });

  it("useFormat follows the active language", () => {
    function N() {
      const f = useFormat();
      return <p data-testid="n">{f.number(1234.5)}</p>;
    }
    mount(<N />);
    expect(screen.getByTestId("n")).toHaveTextContent("1,234.5");
    act(() => setLanguage("es"));
    expect(screen.getByTestId("n").textContent).not.toBe("1,234.5");
  });
});
