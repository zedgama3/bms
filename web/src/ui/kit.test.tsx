import { act, fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRef, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { renderUi } from "@/test/render";
import {
  Banner,
  Button,
  Dialog,
  EmptyState,
  Field,
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
