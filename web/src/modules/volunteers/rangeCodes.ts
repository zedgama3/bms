import type { ApiError, Field } from "@/api";

const isOrder = (f: Field) =>
  f.code === "range" && (f.params?.["min"] === undefined || f.params["max"] === undefined);

/** The service answers `range` for two different problems: a number outside its limits (with
 *  `min` and `max`) and dates in the wrong order (no params). One message cannot word both, so
 *  the second is renamed `range_order` (its own message) before it reaches the form. */
export function withRangeCodes(error: ApiError): ApiError {
  if (!error.fields.some(isOrder)) return error;
  const copy = Object.create(error) as ApiError;
  Object.defineProperty(copy, "fields", {
    value: error.fields.map((f) => (isOrder(f) ? { ...f, code: "range_order" } : f)),
  });
  return copy;
}
