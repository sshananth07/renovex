import { renderHook, act } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useListSearchParams } from "./listParams";

const replaceMock = vi.fn();
let currentSearch = "";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: replaceMock }),
  usePathname: () => "/clients",
  useSearchParams: () => new URLSearchParams(currentSearch),
}));

describe("useListSearchParams", () => {
  afterEach(() => {
    replaceMock.mockClear();
  });

  it("parses page, pageSize, search, sort, order from the URL", () => {
    currentSearch = "page=2&pageSize=50&search=kitchen&sort=name&order=asc";
    const { result } = renderHook(() => useListSearchParams());

    expect(result.current.params).toEqual({
      page: 2,
      pageSize: 50,
      search: "kitchen",
      sort: "name",
      order: "asc",
    });
  });

  it("defaults to page 1 when no page param is present", () => {
    currentSearch = "";
    const { result } = renderHook(() => useListSearchParams());

    expect(result.current.params.page).toBe(1);
  });

  it("resets page to 1 when search changes via setParams", () => {
    currentSearch = "page=3&search=old";
    const { result } = renderHook(() => useListSearchParams());

    act(() => {
      result.current.setParams({ search: "new" });
    });

    const calledWith = replaceMock.mock.calls[0][0] as string;
    expect(calledWith).toContain("page=1");
    expect(calledWith).toContain("search=new");
  });

  it("resets page to 1 when sort changes via setParams", () => {
    currentSearch = "page=3&sort=name&order=asc";
    const { result } = renderHook(() => useListSearchParams());

    act(() => {
      result.current.setParams({ sort: "createdAt" });
    });

    const calledWith = replaceMock.mock.calls[0][0] as string;
    expect(calledWith).toContain("page=1");
    expect(calledWith).toContain("sort=createdAt");
  });

  it("does not reset page when only page itself changes via setParams", () => {
    currentSearch = "page=1&search=kitchen";
    const { result } = renderHook(() => useListSearchParams());

    act(() => {
      result.current.setParams({ page: 4 });
    });

    const calledWith = replaceMock.mock.calls[0][0] as string;
    expect(calledWith).toContain("page=4");
  });
});
