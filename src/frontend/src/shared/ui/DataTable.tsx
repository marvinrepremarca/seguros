import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from "@tanstack/react-table";

/** Tabla de listados sobre TanStack Table. La paginación la hace quien la usa. */
export function DataTable<T>({
  caption,
  columns,
  data,
  rowKey,
}: {
  caption: string;
  // TanStack Table define las columnas con un tipo por celda: `any` es el tipo que documenta.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  columns: ColumnDef<T, any>[];
  data: T[];
  rowKey: (row: T) => string;
}) {
  // eslint-disable-next-line react-hooks/incompatible-library -- TanStack Table v8 no es memoizable por el compilador
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => rowKey(row),
  });
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <caption className="sr-only">{caption}</caption>
        <thead>
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id} className="border-b">
              {group.headers.map((header) => (
                <th key={header.id} scope="col" className="py-2 pr-4">
                  {flexRender(header.column.columnDef.header, header.getContext())}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr key={row.id} className="border-b">
              {row.getVisibleCells().map((cell) => (
                <td key={cell.id} className="py-2 pr-4">
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
