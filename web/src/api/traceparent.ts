function hex(bytes: number): string {
  const buf = new Uint8Array(bytes);
  crypto.getRandomValues(buf);
  let out = "";
  for (const b of buf) out += b.toString(16).padStart(2, "0");
  return out;
}

/** A fresh W3C `traceparent`: 00-<32 hex>-<16 hex>-01 (decision 6). The trace ID is never all zero. */
export function newTraceparent(): { header: string; traceId: string } {
  let traceId = hex(16);
  while (/^0+$/.test(traceId)) traceId = hex(16);
  let spanId = hex(8);
  while (/^0+$/.test(spanId)) spanId = hex(8);
  return { header: `00-${traceId}-${spanId}-01`, traceId };
}
