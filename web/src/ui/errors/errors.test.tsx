import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api";
import type { ApiErrorInit } from "@/api/errors";
import { renderUi } from "@/test/render";
import { expectNoAxeViolations } from "@/test/axe";
import { useT } from "@/i18n";
import { ErrorState, ErrorSummary, messageFor, showsRef, useFieldErrors } from "@/ui";
import { setUntranslatedHandler } from "@/ui/internal";
import en from "@/shell/messages/en.json";

const MARKER = "ZQ9MARKER7";

function err(over: Partial<ApiErrorInit> = {}): ApiError {
  return new ApiError({
    status: 500,
    code: "internal",
    method: "POST",
    route: "/api/hello/greetings",
    module: "hello",
    traceId: "0123456789abcdef0123456789abcdef",
    ...over,
  });
}

afterEach(() => {
  setUntranslatedHandler(() => {});
});

function Say({ error }: { error: ApiError }) {
  const t = useT();
  return <p data-testid="out">{messageFor(t, error)}</p>;
}

describe("messageFor", () => {
  const cases: [string, string][] = [
    ["invalid", "Some of the information isn't right. Check the fields below."],
    ["unauthenticated", "Your session ended. Sign in again."],
    ["permission", "You don't have permission to do that."],
    [
      "not_found",
      "We couldn't find that. It may have been removed, or you may not have access to it.",
    ],
    ["conflict", "That change clashes with another one. Reload and try again."],
    ["read_only", "This copy is read-only right now, so that can't be saved."],
    ["unavailable", "The server isn't available right now. Try again in a moment."],
    ["timeout", "The server isn't available right now. Try again in a moment."],
    ["shutting_down", "The server isn't available right now. Try again in a moment."],
    ["unsupported", "That feature isn't turned on here."],
    ["internal", "Something went wrong."],
    ["payload_too_large", "Something went wrong."],
    ["unsupported_media_type", "Something went wrong."],
    ["method_not_allowed", "Something went wrong."],
    ["cross_origin", "Something went wrong."],
    ["misdirected", "Something went wrong."],
    ["malformed_response", "Something went wrong."],
    ["network", "Can't reach the server. Check your connection and try again."],
  ];
  it.each(cases)("%s", (code, text) => {
    renderUi(<Say error={err({ code })} />);
    expect(screen.getByTestId("out")).toHaveTextContent(text);
  });

  it("rate_limited interpolates the seconds with a plural", () => {
    const { unmount } = renderUi(<Say error={err({ code: "rate_limited", retryAfter: 1 })} />);
    expect(screen.getByTestId("out")).toHaveTextContent("Wait 1 second and try again.");
    unmount();
    renderUi(<Say error={err({ code: "rate_limited", retryAfter: 30 })} />);
    expect(screen.getByTestId("out")).toHaveTextContent("Wait 30 seconds and try again.");
  });

  it("every code in the spec's table has a message", () => {
    for (const [code] of cases) expect(en).toHaveProperty(`error.${code}`);
  });

  it("F11b: an unknown code shows error.unknown and reports once", () => {
    const handler = vi.fn();
    setUntranslatedHandler(handler);
    const e = err({ code: "teapot", status: 418 });
    renderUi(<ErrorState error={e} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Something went wrong.");
    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith("error", "teapot", e);
  });
});

describe("showsRef (decision 9)", () => {
  it.each([
    ["invalid", false],
    ["unauthenticated", false],
    ["read_only", false],
    ["network", false],
    ["permission", true],
    ["not_found", true],
    ["conflict", true],
    ["rate_limited", true],
    ["internal", true],
    ["malformed_response", true],
  ])("%s -> %s when a ref exists", (code, want) => {
    expect(showsRef(err({ code, ref: "1A2B-3C4D" }))).toBe(want);
  });
  it("is false without a ref", () => {
    expect(showsRef(err({ code: "internal" }))).toBe(false);
  });
});

describe("ErrorState", () => {
  it("shows the sentence, the reference line and a retry button", async () => {
    const onRetry = vi.fn();
    const { container } = renderUi(
      <ErrorState error={err({ ref: "1A2B-3C4D" })} onRetry={onRetry} />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Something went wrong.");
    expect(screen.getByText("1A2B-3C4D").tagName).toBe("CODE");
    expect(screen.getByText(/If you ask for help, quote the reference/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
    await expectNoAxeViolations(container);
  });

  it("network has no reference and no retry unless given", () => {
    renderUi(<ErrorState error={err({ code: "network", status: 0, ref: "1A2B-3C4D" })} />);
    expect(screen.queryByText("1A2B-3C4D")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("copies the reference with navigator.clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    renderUi(<ErrorState error={err({ ref: "1A2B-3C4D" })} />);
    await userEvent.click(screen.getByRole("button", { name: "Copy reference" }));
    expect(writeText).toHaveBeenCalledWith("1A2B-3C4D");
    expect(await screen.findByText("Reference copied")).toBeInTheDocument();
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
  });

  it("without a clipboard the reference stays as selectable text and there is no copy button", () => {
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
    renderUi(<ErrorState error={err({ ref: "1A2B-3C4D" })} />);
    expect(screen.queryByRole("button", { name: "Copy reference" })).not.toBeInTheDocument();
    expect(screen.getByText("1A2B-3C4D")).toBeInTheDocument();
  });
});

function Form({ error, extra }: { error: ApiError | null; extra?: boolean }) {
  const ref = useRef<HTMLFormElement>(null);
  return <FormInner error={error} formRef={ref} extra={extra} />;
}

import { Field } from "@/ui";
import type { RefObject } from "react";

function FormInner({
  error,
  formRef,
  extra,
}: {
  error: ApiError | null;
  formRef: RefObject<HTMLFormElement | null>;
  extra?: boolean | undefined;
}) {
  const fe = useFieldErrors(error);
  return (
    <form ref={formRef}>
      <ErrorSummary error={error} formRef={formRef} />
      <Field label="Start" name="start" error={fe["start"]} required />
      {extra ? <Field label="End" name="end" error={fe["end"]} /> : null}
    </form>
  );
}

describe("field messages and the summary", () => {
  it("F33: the summary takes focus, is role=alert, links each error and wires the input", async () => {
    const e = err({
      code: "invalid",
      status: 400,
      fields: [{ field: "start", code: "required", reason: "must not be empty" }],
    });
    const { container, rerender } = renderUi(<Form error={null} />);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    rerender(<Form error={e} />);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveFocus();
    const link = within(alert).getByRole("link", { name: /Start: Required\./ });
    const input = screen.getByLabelText(/Start/);
    expect(link).toHaveAttribute("href", `#${input.id}`);
    expect(input).toHaveAttribute("aria-invalid", "true");
    const describedBy = input.getAttribute("aria-describedby") ?? "";
    expect(document.getElementById(describedBy)).toHaveTextContent("Required.");
    await userEvent.click(link);
    expect(input).toHaveFocus();
    await expectNoAxeViolations(container);
  });

  it("a field with no input appears in the summary only", () => {
    const e = err({
      code: "invalid",
      status: 400,
      fields: [{ field: "nope.path", code: "required", reason: "x" }],
    });
    renderUi(<Form error={e} />);
    const alert = screen.getByRole("alert");
    expect(within(alert).queryByRole("link")).not.toBeInTheDocument();
    expect(alert).toHaveTextContent("Required.");
    expect(screen.getByLabelText(/Start/)).not.toHaveAttribute("aria-invalid");
  });

  it("does not show a reference for invalid", () => {
    renderUi(<Form error={err({ code: "invalid", status: 400, ref: "1A2B-3C4D", fields: [] })} />);
    expect(screen.queryByText("1A2B-3C4D")).not.toBeInTheDocument();
  });

  it("interpolates params (range with min and max)", () => {
    const e = err({
      code: "invalid",
      status: 400,
      fields: [{ field: "start", code: "range", params: { min: "1", max: "10" }, reason: "x" }],
    });
    renderUi(<Form error={e} />);
    expect(document.body).toHaveTextContent("Must be between 1 and 10.");
  });

  it("lookup order: module.<m>.field.<code> wins over field.<code>; an unknown code falls to the wrapper", () => {
    const e = err({
      code: "invalid",
      status: 400,
      fields: [
        { field: "start", code: "required", reason: "must not be empty" },
        { field: "end", code: "overlap", reason: "shifts overlap" },
      ],
    });
    renderUi(<Form error={e} extra />, {
      messages: {
        "module.hello.field.required": "Hello needs this.",
        "module.hello.field.overlap": "Overlaps another shift.",
      },
    });
    expect(
      document.getElementById(
        screen.getByLabelText(/Start/).getAttribute("aria-describedby") ?? "",
      ),
    ).toHaveTextContent("Hello needs this.");
    expect(
      document.getElementById(screen.getByLabelText("End").getAttribute("aria-describedby") ?? ""),
    ).toHaveTextContent("Overlaps another shift.");
    expect(document.querySelector('span[lang="en"]')).toBeNull();
  });

  it("F11: an unknown field code shows the wrapper with the reason marked lang=en and reports once; reason is absent for known codes", async () => {
    const handler = vi.fn();
    setUntranslatedHandler(handler);
    const e = err({
      code: "invalid",
      status: 400,
      fields: [
        { field: "start", code: "overlap", reason: "overlaps the shift that starts at 09:00" },
        { field: "end", code: "required", reason: `must not be empty ${MARKER}` },
      ],
    });
    const { rerender } = renderUi(<Form error={e} extra />);
    const reasonSpans = [...document.querySelectorAll('span[lang="en"]')];
    expect(reasonSpans.map((s) => s.textContent)).toEqual([
      "overlaps the shift that starts at 09:00",
      "overlaps the shift that starts at 09:00",
    ]);
    for (const s of reasonSpans)
      expect(s.parentElement?.textContent).toContain("This value isn't valid: ");
    expect(document.body).not.toHaveTextContent(MARKER);
    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith("field", "overlap", e);
    rerender(<Form error={e} extra />);
    await act(async () => {});
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("an empty code is treated as unknown", () => {
    const e = err({
      code: "invalid",
      status: 400,
      fields: [{ field: "start", code: "", reason: "bad value" }],
    });
    renderUi(<Form error={e} />);
    expect(document.querySelector('span[lang="en"]')).toHaveTextContent("bad value");
  });
});
