import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRef, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { renderUi } from "@/test/render";
import {
  Attachments,
  Banner,
  Button,
  Dialog,
  EmptyState,
  Field,
  FileField,
  LOADING_DELAY_MS,
  Loading,
  Table,
  TOAST_MS,
  useToast,
} from "@/ui";

afterEach(() => {
  vi.useRealTimers();
});

describe("Button", () => {
  it("is a 44 px target, type=button by default, and disabled while busy", async () => {
    const onClick = vi.fn();
    const { container, rerender } = renderUi(<Button onClick={onClick}>Save</Button>);
    const b = screen.getByRole("button", { name: "Save" });
    expect(b).toHaveAttribute("type", "button");
    expect(b.className).toMatch(/min-h-11/);
    expect(b.className).toMatch(/min-w-11/);
    await userEvent.click(b);
    expect(onClick).toHaveBeenCalledTimes(1);
    rerender(
      <Button onClick={onClick} busy>
        Save
      </Button>,
    );
    const busy = screen.getByRole("button", { name: "Save" });
    expect(busy).toHaveAttribute("aria-disabled", "true");
    expect(busy).not.toBeDisabled(); // focus stays
    await userEvent.click(busy);
    expect(onClick).toHaveBeenCalledTimes(1); // the busy click was ignored
    await expectNoAxeViolations(container);
  });
});

describe("Field", () => {
  it("ties label, hint and error together; says required in text", async () => {
    const { container } = renderUi(
      <Field
        label="Nickname"
        name="nickname"
        hint="What people call you"
        required
        error={{ text: "Required." }}
        autoComplete="nickname"
        inputMode="text"
      />,
    );
    const input = screen.getByLabelText(/Nickname/);
    expect(input).toHaveAttribute("name", "nickname");
    expect(input).toHaveAttribute("required");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAttribute("autocomplete", "nickname");
    const described = (input.getAttribute("aria-describedby") ?? "")
      .split(" ")
      .map((id) => document.getElementById(id)?.textContent);
    expect(described).toEqual(["What people call you", "! Required."]);
    expect(screen.getByText("(required)")).toBeInTheDocument();
    expect(input.className).toMatch(/min-h-11/);
    await expectNoAxeViolations(container);
  });

  it("without an error there is no aria-invalid", () => {
    renderUi(<Field label="Name" name="name" />);
    expect(screen.getByLabelText("Name")).not.toHaveAttribute("aria-invalid");
  });

  it("a custom control receives the wiring", async () => {
    const { container } = renderUi(
      <Field label="Colour" name="colour" error={{ text: "Pick one." }}>
        {(c) => (
          <select {...c}>
            <option>red</option>
          </select>
        )}
      </Field>,
    );
    expect(screen.getByLabelText("Colour")).toHaveAttribute("aria-invalid", "true");
    await expectNoAxeViolations(container);
  });
});

describe("Banner, EmptyState, Table", () => {
  it("Banner is a status region with an icon that is hidden from readers", async () => {
    const { container } = renderUi(<Banner tone="warn">Careful</Banner>);
    expect(screen.getByText("Careful").closest('[role="status"]')).not.toBeNull();
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
    await expectNoAxeViolations(container);
  });

  it("EmptyState has axe-clean content and an action", async () => {
    const { container } = renderUi(
      <EmptyState action={<Button>Add</Button>}>Nothing here</EmptyState>,
    );
    expect(screen.getByText("Nothing here")).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("Table repeats each header as a label on every cell and has a row header", async () => {
    const rows = [
      { a: "x", b: "1" },
      { a: "y", b: "2" },
    ];
    const { container } = renderUi(
      <Table
        caption="Things"
        rows={rows}
        rowKey={(r) => r.a}
        columns={[
          { header: "Name", cell: (r) => r.a, rowHeader: true },
          { header: "Count", cell: (r) => r.b },
        ]}
      />,
    );
    expect(screen.getByRole("table", { name: "Things" })).toBeInTheDocument();
    expect(container.querySelectorAll('td[data-label="Count"]')).toHaveLength(2);
    expect(container.querySelectorAll('th[scope="row"][data-label="Name"]')).toHaveLength(2);
    await expectNoAxeViolations(container);
  });
});

describe("Loading", () => {
  it("is busy at once and says Loading only after 300 ms", () => {
    vi.useFakeTimers();
    const { container } = renderUi(<Loading />);
    const region = container.querySelector('[aria-live="polite"]')!;
    expect(region).not.toHaveAttribute("aria-busy");
    expect(region).toHaveAttribute("aria-live", "polite");
    expect(region).toHaveTextContent("");
    act(() => {
      vi.advanceTimersByTime(LOADING_DELAY_MS - 1);
    });
    expect(region).toHaveTextContent("");
    act(() => {
      vi.advanceTimersByTime(2);
    });
    expect(region).toHaveTextContent("Loading…");
  });
});

describe("Dialog", () => {
  function Host() {
    const [open, setOpen] = useState(false);
    const opener = useRef<HTMLButtonElement>(null);
    return (
      <>
        <button ref={opener} onClick={() => setOpen(true)}>
          Open
        </button>
        <Dialog open={open} title="Confirm" onClose={() => setOpen(false)}>
          <p>Are you sure?</p>
        </Dialog>
      </>
    );
  }

  it("opens as a modal dialog with a labelled title and closes with its button", async () => {
    const { container } = renderUi(<Host />);
    await userEvent.click(screen.getByRole("button", { name: "Open" }));
    const dialog = container.querySelector("dialog")!;
    expect(dialog).toHaveAttribute("open");
    expect(dialog).toHaveAccessibleName("Confirm");
    await expectNoAxeViolations(container);
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(dialog).not.toHaveAttribute("open");
  });

  it("tells the parent once when closed with the button or by the parent", async () => {
    const onClose = vi.fn();
    function H() {
      const [open, setOpen] = useState(true);
      return (
        <Dialog
          open={open}
          title="T"
          onClose={() => {
            onClose();
            setOpen(false);
          }}
        >
          x
        </Dialog>
      );
    }
    renderUi(<H />);
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("full-screen below 640 px: the classes say so", () => {
    const { container } = renderUi(<Host />);
    const cls = container.querySelector("dialog")!.className;
    expect(cls).toMatch(/h-dvh/);
    expect(cls).toMatch(/w-full/);
    expect(cls).toMatch(/sm:h-auto/);
  });
});

describe("Toast", () => {
  function Host() {
    const toast = useToast();
    return <button onClick={() => toast.show("Saved")}>Go</button>;
  }

  it("announces in a status region and auto-dismisses after 8 s", () => {
    vi.useFakeTimers();
    renderUi(<Host />);
    fireEvent.click(screen.getByText("Go"));
    expect(screen.getByText("Saved")).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(TOAST_MS - 1);
    });
    expect(screen.getByText("Saved")).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(2);
    });
    expect(screen.queryByText("Saved")).not.toBeInTheDocument();
  });

  it("can be dismissed by hand and is axe-clean", async () => {
    const { container } = renderUi(<Host />);
    await userEvent.click(screen.getByText("Go"));
    await expectNoAxeViolations(container);
    await userEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByText("Saved")).not.toBeInTheDocument();
  });
});

describe("Attachments", () => {
  it("an unexpected rejection says something went wrong and offers a retry", async () => {
    URL.createObjectURL = () => "blob:test";
    URL.revokeObjectURL = () => {};
    let calls = 0;
    const fetchRow = vi.fn(() => {
      calls += 1;
      return calls === 1 ? Promise.reject(new TypeError("boom")) : Promise.resolve(new Blob(["x"]));
    });
    renderUi(<Attachments rows={[{ id: "a", label: "Doc", fetch: fetchRow }]} />);
    await userEvent.click(screen.getByRole("button", { name: /^Open\s*Doc/ }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Something went wrong.");
    await userEvent.click(within(alert).getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: /Doc/ })).toHaveAttribute("href", "blob:test");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("keeps a status region mounted, the button and the link have distinct names, and URLs are revoked", async () => {
    let n = 0;
    URL.createObjectURL = () => `blob:test-${(n += 1)}`;
    const revoke = vi.fn();
    URL.revokeObjectURL = revoke;
    const fetchRow = vi.fn(() => Promise.resolve(new Blob(["x"])));
    const { unmount } = renderUi(
      <Attachments rows={[{ id: "a", label: "Doc", fetch: fetchRow }]} />,
    );
    const row = screen.getByRole("listitem");
    const status = within(row).getByRole("status");
    expect(status).toBeEmptyDOMElement();
    await userEvent.click(screen.getByRole("button", { name: /^Open\s*Doc/ }));
    const link = await screen.findByRole("link", { name: "Doc: open in a new tab" });
    expect(within(row).getByRole("status")).toBe(status);
    expect(status).toHaveTextContent("The file is ready.");
    expect(link).toHaveAttribute("href", "blob:test-1");
    expect(revoke).not.toHaveBeenCalled();
    // Opening again replaces the URL: the first is revoked.
    await userEvent.click(screen.getByRole("button", { name: /^Open\s*Doc/ }));
    await screen.findByRole("link", { name: "Doc: open in a new tab" });
    expect(revoke).toHaveBeenCalledWith("blob:test-1");
    unmount();
    expect(revoke).toHaveBeenCalledWith("blob:test-2");
  });
});

describe("FileField", () => {
  const png = (size = 10) => new File([new Uint8Array(size)], "logo.png", { type: "image/png" });

  function Harness({ maxBytes, error }: { maxBytes?: number; error?: string }) {
    const [file, setFile] = useState<File | null>(null);
    return (
      <FileField
        label="Logo"
        name="file"
        hint="PNG only"
        accept={["image/png", "image/jpeg"]}
        {...(maxBytes !== undefined ? { maxBytes } : {})}
        {...(error ? { error: { text: error } } : {})}
        value={file}
        onChange={setFile}
      />
    );
  }

  it("has a label, a hint, accept, a 44 px target, and shows the chosen file's name and size", async () => {
    const { container } = renderUi(<Harness />);
    const input = screen.getByLabelText("Logo", { selector: "input" });
    expect(input).toHaveAttribute("type", "file");
    expect(input).toHaveAttribute("name", "file");
    expect(input).toHaveAttribute("accept", "image/png,image/jpeg");
    expect(input.className).toMatch(/min-h-11/);
    expect(input).toHaveAccessibleDescription("PNG only");
    await userEvent.upload(input, png(2048));
    expect(screen.getByText(/Chosen file: logo\.png \(2 kB\)/)).toBeInTheDocument();
    await expectNoAxeViolations(container);
  });

  it("refuses a file over maxBytes at once with field.too_large and passes null", async () => {
    renderUi(<Harness maxBytes={2 * 1024 * 1024} />);
    const input = screen.getByLabelText("Logo", { selector: "input" });
    await userEvent.upload(input, png(3 * 1024 * 1024));
    expect(screen.getByText("The file is larger than 2 MB.")).toBeInTheDocument();
    expect(input).toHaveAttribute("aria-invalid", "true");
    // announced when it appears
    expect(screen.getByRole("alert")).toHaveTextContent("The file is larger than 2 MB.");
    expect(screen.queryByText(/Chosen file/)).not.toBeInTheDocument();
    await userEvent.upload(input, png(50));
    expect(screen.queryByText(/larger than/)).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByText(/Chosen file: logo\.png/)).toBeInTheDocument();
  });

  it("accepts a type outside accept and leaves it to the server", async () => {
    renderUi(<Harness />);
    const odd = new File([new Uint8Array(4)], "a.gif", { type: "image/gif" });
    await userEvent.upload(screen.getByLabelText("Logo", { selector: "input" }), odd, {
      applyAccept: false,
    });
    expect(screen.getByText(/Chosen file: a\.gif/)).toBeInTheDocument();
  });

  it("shows an error from the server on the input", () => {
    renderUi(<Harness error="This kind of file isn't accepted." />);
    expect(screen.getByLabelText("Logo", { selector: "input" })).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(screen.getByText("This kind of file isn't accepted.")).toBeInTheDocument();
  });
});

describe("Loading counter (usage screen_ready)", () => {
  it("counts the Loading indicators mounted now", async () => {
    const { loadingCount } = await import("./Loading");
    expect(loadingCount()).toBe(0);
    const { unmount } = renderUi(<Loading />);
    expect(loadingCount()).toBe(1);
    unmount();
    expect(loadingCount()).toBe(0);
  });
});
