/* eslint-disable jsx-a11y/no-redundant-roles, jsx-a11y/no-interactive-element-to-noninteractive-role -- the roles are deliberate: the stacked phone layout uses display: block, which drops native table semantics in some browsers */
import type { ReactNode } from "react";

export interface Column<Row> {
  header: string;
  cell: (row: Row) => ReactNode;
  /** True for the column that names the row (rendered as the row header). */
  rowHeader?: boolean;
}

/** A table that stacks into labelled rows below 640 px: every cell repeats its header as a
 *  label (no horizontal scroll at 320 px). */
export function Table<Row>({
  caption,
  columns,
  rows,
  rowKey,
}: {
  caption: string;
  columns: readonly Column<Row>[];
  rows: readonly Row[];
  rowKey: (row: Row) => string;
}) {
  return (
    <table role="table" className="block w-full border-collapse sm:table">
      <caption className="mb-2 block text-start sm:table-caption font-semibold">{caption}</caption>
      {/* Hidden on a phone (display: none): each cell repeats its header as a visible label, so
          a screen reader hears it once. Roles are explicit because display: block drops the
          native table semantics in some browsers. */}
      <thead role="rowgroup" className="hidden sm:table-header-group">
        <tr role="row" className="sm:table-row">
          {columns.map((c) => (
            <th
              key={c.header}
              role="columnheader"
              scope="col"
              className="border-b border-border px-3 py-2 text-start font-semibold break-normal"
            >
              {c.header}
            </th>
          ))}
        </tr>
      </thead>
      <tbody role="rowgroup" className="block sm:table-row-group">
        {rows.map((row) => (
          <tr
            role="row"
            key={rowKey(row)}
            className="mb-3 block rounded-md border border-border sm:mb-0 sm:table-row sm:rounded-none sm:border-0"
          >
            {columns.map((c) => {
              const cellClass =
                "block px-3 py-2 before:block before:text-sm before:font-semibold before:text-muted before:content-[attr(data-label)] sm:table-cell sm:border-b sm:border-border sm:before:hidden";
              return c.rowHeader ? (
                <th
                  key={c.header}
                  role="rowheader"
                  scope="row"
                  data-label={c.header}
                  className={`${cellClass} text-start`}
                >
                  {c.cell(row)}
                </th>
              ) : (
                <td key={c.header} role="cell" data-label={c.header} className={cellClass}>
                  {c.cell(row)}
                </td>
              );
            })}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
