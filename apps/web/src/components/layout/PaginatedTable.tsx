"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export interface PaginatedTableColumn<T> {
  key: string;
  label: string;
  sortable?: boolean;
  render?: (row: T) => React.ReactNode;
}

interface PaginatedTableProps<T> {
  columns: PaginatedTableColumn<T>[];
  rows: T[];
  getRowKey: (row: T) => string;
  page: number;
  pageSize: number;
  total: number;
  isLoading?: boolean;
  error?: string;
  emptyMessage?: string;
  onPageChange: (page: number) => void;
  onSortChange?: (columnKey: string) => void;
  onRowClick?: (row: T) => void;
}

// Presentation-only shared list primitive (architecture doc §18): renders
// whatever data/loading/error state its feature-owned caller already
// fetched via TanStack Query. It never calls an endpoint, never knows about
// business filters, and is reused as-is by Clients/Projects/Spaces/Work
// Items — the only per-resource thing is the `columns` definition each
// feature passes in.
export function PaginatedTable<T>({
  columns,
  rows,
  getRowKey,
  page,
  pageSize,
  total,
  isLoading,
  error,
  emptyMessage = "No results.",
  onPageChange,
  onSortChange,
  onRowClick,
}: PaginatedTableProps<T>) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className="flex flex-col gap-3">
      <p className="px-1 text-[0.6875rem] text-muted-foreground sm:hidden">Swipe table to view all columns →</p>
      <div className="surface-card overflow-hidden">
        <Table className="min-w-[42rem]">
          <TableHeader>
            <TableRow>
              {columns.map((column) =>
                column.sortable ? (
                  <TableHead key={column.key}>
                    <button
                      type="button"
                      onClick={() => onSortChange?.(column.key)}
                      className="font-medium text-muted-foreground hover:text-foreground"
                    >
                      {column.label}
                    </button>
                  </TableHead>
                ) : (
                  <TableHead key={column.key}>{column.label}</TableHead>
                )
              )}
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: 5 }).map((_, index) => (
                <TableRow key={index} data-testid="paginated-table-skeleton-row">
                  {columns.map((column) => (
                    <TableCell key={column.key}>
                      <Skeleton className="h-4 w-full max-w-40" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : error ? (
              <TableRow>
                <TableCell colSpan={columns.length} className="sticky left-0 bg-card px-4 py-8 text-left text-sm text-destructive sm:static sm:text-center">
                  {error}
                </TableCell>
              </TableRow>
            ) : rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={columns.length} className="sticky left-0 bg-card px-4 py-8 text-left text-sm text-muted-foreground sm:static sm:text-center">
                  {emptyMessage}
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => (
                <TableRow
                  key={getRowKey(row)}
                  onClick={onRowClick ? () => onRowClick(row) : undefined}
                  className={onRowClick ? "cursor-pointer" : undefined}
                >
                  {columns.map((column) => (
                    <TableCell key={column.key}>
                      {column.render ? column.render(row) : String((row as Record<string, unknown>)[column.key] ?? "")}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-between px-1 text-xs text-muted-foreground">
        <span>
          {total === 0 ? "No results" : `Showing ${(page - 1) * pageSize + 1}–${Math.min(page * pageSize, total)} of ${total}`}
        </span>
        <div className="flex items-center gap-2">
          <span className="hidden sm:inline">Page {page} of {totalPages}</span>
          <Button
            variant="outline"
            size="icon-sm"
            aria-label="Previous page"
            disabled={page <= 1}
            onClick={() => onPageChange(page - 1)}
          >
            <ChevronLeft className="size-4" />
          </Button>
          <Button
            variant="outline"
            size="icon-sm"
            aria-label="Next page"
            disabled={page >= totalPages}
            onClick={() => onPageChange(page + 1)}
          >
            <ChevronRight className="size-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}
