import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { PaginatedTable } from "./PaginatedTable";

interface Row {
  id: string;
  name: string;
}

const columns = [
  { key: "name", label: "Name", sortable: true },
];

describe("PaginatedTable", () => {
  it("renders rows using the provided columns", () => {
    render(
      <PaginatedTable<Row>
        columns={columns}
        rows={[{ id: "1", name: "Ahmad Residence" }]}
        getRowKey={(row) => row.id}
        page={1}
        pageSize={25}
        total={1}
        onPageChange={vi.fn()}
      />
    );

    expect(screen.getByText("Ahmad Residence")).toBeInTheDocument();
    expect(screen.getByText("Name")).toBeInTheDocument();
  });

  it("shows a skeleton, not a spinner, while loading", () => {
    render(
      <PaginatedTable<Row>
        columns={columns}
        rows={[]}
        getRowKey={(row) => row.id}
        page={1}
        pageSize={25}
        total={0}
        isLoading
        onPageChange={vi.fn()}
      />
    );

    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getAllByTestId("paginated-table-skeleton-row").length).toBeGreaterThan(0);
  });

  it("shows an empty state when there are no rows and not loading", () => {
    render(
      <PaginatedTable<Row>
        columns={columns}
        rows={[]}
        getRowKey={(row) => row.id}
        page={1}
        pageSize={25}
        total={0}
        emptyMessage="No clients yet."
        onPageChange={vi.fn()}
      />
    );

    expect(screen.getByText("No clients yet.")).toBeInTheDocument();
  });

  it("shows an error state and does not render rows", () => {
    render(
      <PaginatedTable<Row>
        columns={columns}
        rows={[]}
        getRowKey={(row) => row.id}
        page={1}
        pageSize={25}
        total={0}
        error="Could not load clients."
        onPageChange={vi.fn()}
      />
    );

    expect(screen.getByText("Could not load clients.")).toBeInTheDocument();
  });

  it("calls onSortChange when a sortable column header is clicked", async () => {
    const onSortChange = vi.fn();
    const user = userEvent.setup();
    render(
      <PaginatedTable<Row>
        columns={columns}
        rows={[{ id: "1", name: "Ahmad Residence" }]}
        getRowKey={(row) => row.id}
        page={1}
        pageSize={25}
        total={1}
        onPageChange={vi.fn()}
        onSortChange={onSortChange}
      />
    );

    await user.click(screen.getByRole("button", { name: "Name" }));
    expect(onSortChange).toHaveBeenCalledWith("name");
  });

  it("calls onPageChange with the next page when the next-page control is used", async () => {
    const onPageChange = vi.fn();
    const user = userEvent.setup();
    render(
      <PaginatedTable<Row>
        columns={columns}
        rows={[{ id: "1", name: "Ahmad Residence" }]}
        getRowKey={(row) => row.id}
        page={1}
        pageSize={1}
        total={2}
        onPageChange={onPageChange}
      />
    );

    await user.click(screen.getByRole("button", { name: /next page/i }));
    expect(onPageChange).toHaveBeenCalledWith(2);
  });

  it("disables the previous-page control on the first page", () => {
    render(
      <PaginatedTable<Row>
        columns={columns}
        rows={[{ id: "1", name: "Ahmad Residence" }]}
        getRowKey={(row) => row.id}
        page={1}
        pageSize={1}
        total={2}
        onPageChange={vi.fn()}
      />
    );

    expect(screen.getByRole("button", { name: /previous page/i })).toBeDisabled();
  });
});
