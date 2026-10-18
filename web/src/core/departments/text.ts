/** A form value as a string (a file or a missing field is ""). */
export const text = (v: FormDataEntryValue | null): string => (typeof v === "string" ? v : "");
