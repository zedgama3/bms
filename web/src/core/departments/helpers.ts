export { holdsSite, holdsAt } from "@/api";

/** The first line of a description, for a card. */
export function firstLine(description: string): string {
  return description.split("\n", 1)[0]?.trim() ?? "";
}
