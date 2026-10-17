import axe from "axe-core";
import { expect } from "vitest";

/** Fails the test when axe finds a violation in the container (WCAG 2.x A/AA rules). */
export async function expectNoAxeViolations(container: Element = document.body): Promise<void> {
  const results = await axe.run(container, {
    runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"] },
    // jsdom has no layout or colours: contrast is checked by tokens.test.ts and in the browser run.
    rules: { "color-contrast": { enabled: false }, region: { enabled: false } },
  });
  const summary = results.violations.map(
    (v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`,
  );
  expect(summary).toEqual([]);
}
