import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { useMemo } from "react";

import styles from "./UI.module.css";

export interface TableRecord {
  readonly id: string;
  readonly [key: string]: unknown;
}

interface DataTableProps {
  readonly caption: string;
  readonly rows: readonly TableRecord[];
  readonly columns: readonly { key: string; label: string }[];
}

/** DataTable is the project-owned TanStack table boundary. */
export function DataTable({ caption, rows, columns }: DataTableProps) {
  const data = useMemo(() => [...rows], [rows]);
  const definitions = useMemo(
    () =>
      columns.map(({ key, label }) =>
        createColumnHelper<TableRecord>().accessor(
          (row) => String(row[key] ?? "—"),
          {
            id: key,
            header: label,
            cell: (context) => context.getValue(),
          },
        ),
      ),
    [columns],
  );
  // eslint-disable-next-line react-hooks/incompatible-library -- TanStack Table intentionally owns this project boundary.
  const table = useReactTable({
    data,
    columns: definitions,
    getCoreRowModel: getCoreRowModel(),
  });
  return (
    <div className={styles.tableFrame} tabIndex={0}>
      <table>
        <caption>{caption}</caption>
        <thead>
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id}>
              {group.headers.map((header) => (
                <th key={header.id} scope="col">
                  {flexRender(
                    header.column.columnDef.header,
                    header.getContext(),
                  )}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr key={row.id}>
              {row.getVisibleCells().map((cell) => (
                <td key={cell.id}>
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
