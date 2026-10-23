import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { expectNoAxeViolations } from "@/test/axe";
import { json, problem } from "@/test/fakeFetch";
import { DEPT, mount, ME, QUAL, queryOf, RECORD, seen, shift, SHIFT, SIGNUP, V } from "./testkit";

const entry = (over: Record<string, unknown> = {}) => ({
  id: SIGNUP,
  status: "booked",
  shift: shift(),
  department_name: "Gate",
  position_name: "Greeter",
  ...over,
});
const meRoutes = (over: Record<string, (r: Request) => Response | Promise<Response>> = {}) => ({
  [`GET ${V}/me/schedule`]: () => json({ items: [entry()] }),
  [`GET ${V}/me/departments`]: () => json({ items: [{ id: "m1", department: DEPT, person: ME }] }),
  [`GET ${V}/people/${ME}/qualifications`]: () => json({ items: [], self_upload: true }),
  [`GET ${V}/me/listing`]: () => json({ listed: true }),
  ...over,
});

describe("volunteers: My volunteering", () => {
  it("shows bookings, departments, qualifications and the listing switch; axe clean", async () => {
    const { container } = mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`GET ${V}/people/${ME}/qualifications`]: () =>
          json({
            items: [
              {
                id: "r1",
                qualification: QUAL,
                person: ME,
                acquired_on: "2025-01-02",
                expires_on: "2099-01-02",
                verified_at: "2025-01-03T00:00:00Z",
                verified_by: "x",
              },
              {
                id: "r2",
                qualification: QUAL,
                person: ME,
                acquired_on: "2020-01-02",
                expires_on: "2021-01-02",
                verified_at: "2020-01-03T00:00:00Z",
                verified_by: "x",
              },
              {
                id: RECORD,
                qualification: QUAL,
                person: ME,
                acquired_on: "2026-01-02",
                certificate: "f1",
              },
            ],
          }),
      }),
    });
    expect(
      await screen.findByRole("heading", { level: 1, name: "My volunteering" }),
    ).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "Morning dirt" })).toBeInTheDocument();
    expect(await screen.findByText("Gate · Greeter")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Leave/ })).toBeInTheDocument();
    expect(await screen.findByText("Verified")).toBeInTheDocument();
    expect(screen.getByText("Expired")).toBeInTheDocument();
    expect(screen.getByText("Waiting for a lead to verify")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /^Open\s*Certificate: First aid/ }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Show my playa name to others on my shifts")).toBeChecked();
    await expectNoAxeViolations(container);
  });

  it("no shifts: a link to Shifts; each section's failure is shown on its own", async () => {
    mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`GET ${V}/me/schedule`]: () => json({}),
        [`GET ${V}/me/departments`]: () => problem(500, { code: "internal" }),
      }),
    });
    expect(await screen.findByText("You have no shifts booked.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Find shifts" })).toHaveAttribute(
      "href",
      "/volunteers",
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong.");
    expect(await screen.findByText("You have no qualifications on record.")).toBeInTheDocument();
  });

  it("the Past tab asks the server for past bookings; Upcoming asks for none", async () => {
    const { fetchMock } = mount({ path: "/volunteers/me", routes: meRoutes() });
    const upcoming = await screen.findByRole("button", { name: "Upcoming" });
    expect(upcoming).toHaveAttribute("aria-pressed", "true");
    expect(queryOf(fetchMock, "GET", `${V}/me/schedule`)?.get("past")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Past" }));
    await waitFor(() =>
      expect(queryOf(fetchMock, "GET", `${V}/me/schedule`)?.get("past")).toBe("true"),
    );
    expect(screen.getByRole("button", { name: "Past" })).toHaveAttribute("aria-pressed", "true");
  });

  it("a failed background refetch of loaded bookings is said in place with a retry", async () => {
    let fail = false;
    mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`GET ${V}/me/schedule`]: () =>
          fail ? problem(500, { code: "internal" }) : json({ items: [entry()] }),
        [`DELETE ${V}/signups/${SIGNUP}`]: () => {
          fail = true;
          return new Response(null, { status: 204 });
        },
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Cancel booking/ }));
    const dialog = await screen.findByRole("dialog", { name: "Cancel your booking?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel my booking" }));
    // the stale row stays visible, so the failure must be said beside it
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Something went wrong.");
    fail = false;
    await userEvent.click(within(alert).getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  });

  it("announces the swapped list when the toggle changes", async () => {
    mount({ path: "/volunteers/me", routes: meRoutes() });
    await screen.findByRole("button", { name: /^Cancel booking/ });
    await userEvent.click(screen.getByRole("button", { name: "Past" }));
    await waitFor(() => expect(screen.getByText("Showing past bookings.")).toBeInTheDocument());
  });

  it("an empty past tab says so without offering Find shifts", async () => {
    mount({
      path: "/volunteers/me",
      routes: meRoutes({ [`GET ${V}/me/schedule`]: () => json({ items: [] }) }),
    });
    await userEvent.click(await screen.findByRole("button", { name: "Past" }));
    expect(await screen.findByText("You have no past shifts.")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Find shifts" })).not.toBeInTheDocument();
  });

  it("a booking someone else canceled is struck through with its reason and has no cancel button", async () => {
    mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`GET ${V}/me/schedule`]: () =>
          json({
            items: [
              entry({ id: "a", status: "canceled", cancel_reason: "lead" }),
              entry({
                id: "b",
                status: "canceled",
                cancel_reason: "department_archived",
                shift: shift({ id: "s2", label: "Other shift" }),
              }),
            ],
          }),
      }),
    });
    const link = await screen.findByRole("link", { name: "Morning dirt" });
    expect(link.closest("h3")).toHaveClass("line-through");
    expect(screen.getByText("Removed by a lead")).toBeInTheDocument();
    expect(screen.getByText("Department archived")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Cancel booking/ })).not.toBeInTheDocument();
  });

  it("Show more appends the next page of bookings", async () => {
    mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`GET ${V}/me/schedule`]: (req) =>
          new URL(req.url).searchParams.get("after") === "c1"
            ? json({
                items: [entry({ id: "z", shift: shift({ id: "s9", label: "Later shift" }) })],
              })
            : json({ items: [entry()], next: "c1" }),
      }),
    });
    await screen.findByRole("link", { name: "Morning dirt" });
    await userEvent.click(screen.getByRole("button", { name: "Show more" }));
    expect(await screen.findByRole("link", { name: "Later shift" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
  });

  it("the upload form is hidden unless the person's own list says self_upload is on", async () => {
    mount({
      path: "/volunteers/me",
      routes: meRoutes({ [`GET ${V}/people/${ME}/qualifications`]: () => json({ items: [] }) }),
    });
    await screen.findByText("You have no qualifications on record.");
    expect(screen.queryByRole("heading", { name: "Upload a certificate" })).not.toBeInTheDocument();
  });

  it("a certificate opens through the Attachments row; 404 and 503 are said in place", async () => {
    URL.createObjectURL = () => "blob:test";
    URL.revokeObjectURL = () => {};
    let status = 200;
    const { fetchMock } = mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`GET ${V}/people/${ME}/qualifications`]: () =>
          json({
            items: [
              {
                id: RECORD,
                qualification: QUAL,
                person: ME,
                acquired_on: "2026-01-02",
                certificate: "f1",
              },
            ],
            self_upload: true,
          }),
        [`GET ${V}/qualification-records/${RECORD}/certificate`]: () =>
          status === 404
            ? problem(404, { code: "not_found" })
            : status === 503
              ? problem(503, { code: "unavailable" }, { "Retry-After": "30" })
              : new Response(new Blob(["%PDF"]), {
                  status: 200,
                  headers: { "Content-Type": "application/pdf" },
                }),
      }),
    });
    const open = await screen.findByRole("button", { name: /^Open\s*Certificate/ });
    await userEvent.click(open);
    await waitFor(() =>
      expect(seen(fetchMock, "GET", `${V}/qualification-records/${RECORD}/certificate`)).toBe(true),
    );
    expect(screen.queryByText(/no longer available/)).not.toBeInTheDocument();
    // the new tab comes from a fresh tap on a real link, not from a script after the await
    const link = await screen.findByRole("link", { name: /^Open Certificate/ });
    expect(link).toHaveAttribute("href", "blob:test");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener");
    expect(link).toHaveFocus();
    expect(link.closest("[role=status]")).toHaveTextContent("The file is ready.");
    status = 404;
    await userEvent.click(open);
    expect(screen.queryByRole("link", { name: /^Open Certificate/ })).not.toBeInTheDocument();
    expect(await screen.findByText("This certificate is no longer available.")).toBeInTheDocument();
    status = 503;
    await userEvent.click(open);
    expect(await screen.findByText(/not available on this copy yet/)).toBeInTheDocument();
  });

  it("cancel a booking after confirming", async () => {
    const { fetchMock } = mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`DELETE ${V}/signups/${SIGNUP}`]: () => new Response(null, { status: 204 }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Cancel booking/ }));
    const dialog = await screen.findByRole("dialog", { name: "Cancel your booking?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel my booking" }));
    await waitFor(() => expect(seen(fetchMock, "DELETE", `${V}/signups/${SIGNUP}`)).toBe(true));
  });

  it("leave a department after confirming", async () => {
    const { fetchMock } = mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`DELETE ${V}/me/departments/${DEPT}`]: () => new Response(null, { status: 204 }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Leave/ }));
    const dialog = await screen.findByRole("dialog", { name: "Leave this department?" });
    expect(dialog).toHaveTextContent("You'll leave Gate.");
    await userEvent.click(within(dialog).getByRole("button", { name: "Leave department" }));
    await waitFor(() =>
      expect(seen(fetchMock, "DELETE", `${V}/me/departments/${DEPT}`)).toBe(true),
    );
  });

  it("the listing switch saves with PUT and shows a failure", async () => {
    let fail = false;
    const { fetchMock } = mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`PUT ${V}/me/listing`]: () =>
          fail ? problem(500, { code: "internal" }) : new Response(null, { status: 204 }),
      }),
    });
    const sw = await screen.findByLabelText("Show my playa name to others on my shifts");
    await userEvent.click(sw);
    await waitFor(() => expect(seen(fetchMock, "PUT", `${V}/me/listing`)).toBe(true));
    const put = fetchMock.mock.calls.find(([r]) => r.method === "PUT")![0];
    expect(await put.clone().json()).toEqual({ listed: false });
    fail = true;
    await userEvent.click(sw);
    expect(await screen.findByRole("alert")).toBeInTheDocument();
  });

  it("an absent listed flag reads as not listed", async () => {
    mount({
      path: "/volunteers/me",
      routes: meRoutes({ [`GET ${V}/me/listing`]: () => json({}) }),
    });
    expect(
      await screen.findByLabelText("Show my playa name to others on my shifts"),
    ).not.toBeChecked();
  });

  it("uploads a certificate as multipart with the dates, then refreshes", async () => {
    const { fetchMock } = mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`POST ${V}/me/qualifications`]: () => json({ id: RECORD }, { status: 201 }),
      }),
    });
    await screen.findByRole("heading", { name: "Upload a certificate" });
    await screen.findByRole("option", { name: "First aid" });
    await userEvent.selectOptions(screen.getByLabelText(/^Qualification/), QUAL);
    await userEvent.upload(
      screen.getByLabelText(/^Certificate/),
      new File(["x"], "cert.pdf", { type: "application/pdf" }),
    );
    await userEvent.type(screen.getByLabelText(/^Acquired on/), "2026-03-04");
    await userEvent.click(screen.getByRole("button", { name: "Upload certificate" }));
    await waitFor(() => expect(seen(fetchMock, "POST", `${V}/me/qualifications`)).toBe(true));
    const req = fetchMock.mock.calls.find(([r]) => r.method === "POST")![0];
    const body = await req.clone().text();
    expect(body).toContain(`name="qualification"\r\n\r\n${QUAL}`);
    expect(body).toContain('name="acquired_on"\r\n\r\n2026-03-04');
    expect(body).toContain('name="file"');
    expect(await screen.findAllByText("Certificate uploaded")).not.toHaveLength(0);
  });

  it("upload with no file lands the error on the file input; a field error lands on its input", async () => {
    mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`POST ${V}/me/qualifications`]: () =>
          problem(400, {
            code: "invalid",
            fields: [{ field: "acquired_on", code: "malformed", reason: "" }],
          }),
      }),
    });
    await screen.findByRole("heading", { name: "Upload a certificate" });
    await userEvent.click(screen.getByRole("button", { name: "Upload certificate" }));
    expect(screen.getByLabelText(/^Certificate/)).toHaveAttribute("aria-invalid", "true");
    await screen.findByRole("option", { name: "First aid" });
    await userEvent.selectOptions(screen.getByLabelText(/^Qualification/), QUAL);
    await userEvent.upload(
      screen.getByLabelText(/^Certificate/),
      new File(["x"], "c.png", { type: "image/png" }),
    );
    await userEvent.type(screen.getByLabelText(/^Acquired on/), "2026-03-04");
    await userEvent.click(screen.getByRole("button", { name: "Upload certificate" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Some of the information isn't right",
    );
    expect(screen.getByLabelText(/^Acquired on/)).toHaveAttribute("aria-invalid", "true");
  });

  it("when self-upload is off (unsupported) the form gives way to a note", async () => {
    mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`POST ${V}/me/qualifications`]: () => problem(501, { code: "unsupported" }),
      }),
    });
    await screen.findByRole("heading", { name: "Upload a certificate" });
    await screen.findByRole("option", { name: "First aid" });
    await userEvent.selectOptions(screen.getByLabelText(/^Qualification/), QUAL);
    await userEvent.upload(
      screen.getByLabelText(/^Certificate/),
      new File(["x"], "c.png", { type: "image/png" }),
    );
    await userEvent.type(screen.getByLabelText(/^Acquired on/), "2026-03-04");
    await userEvent.click(screen.getByRole("button", { name: "Upload certificate" }));
    expect(
      await screen.findByText(/Uploading certificates is switched off here/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Upload certificate" })).not.toBeInTheDocument();
  });

  it("withdraw a pending certificate after confirming", async () => {
    const { fetchMock } = mount({
      path: "/volunteers/me",
      routes: meRoutes({
        [`GET ${V}/people/${ME}/qualifications`]: () =>
          json({
            items: [{ id: RECORD, qualification: QUAL, person: ME, acquired_on: "2026-01-02" }],
          }),
        [`DELETE ${V}/qualification-records/${RECORD}`]: () => new Response(null, { status: 204 }),
      }),
    });
    await userEvent.click(await screen.findByRole("button", { name: /^Withdraw/ }));
    const dialog = await screen.findByRole("dialog", { name: "Withdraw this certificate?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Withdraw certificate" }));
    await waitFor(() =>
      expect(seen(fetchMock, "DELETE", `${V}/qualification-records/${RECORD}`)).toBe(true),
    );
    void SHIFT;
  });
});
