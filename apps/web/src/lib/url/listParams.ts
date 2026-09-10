import { usePathname, useRouter, useSearchParams } from "next/navigation";

export interface ListParams {
  page: number;
  pageSize?: number;
  search?: string;
  sort?: string;
  order?: "asc" | "desc";
}

// Page/search/sort/order for every canonically-paginated list screen (F1
// architecture doc §7.3/§17) live in the URL, not in component state, so
// list state survives reload/back-forward/sharing and every query is
// derivable from the URL alone. This hook owns ONLY that URL-derived
// parsing/writing — the actual data fetch belongs to each feature's
// TanStack Query hook, which reads `params` from here as its query key.
export function useListSearchParams() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const page = Number(searchParams.get("page") ?? "1") || 1;
  const pageSizeRaw = searchParams.get("pageSize");
  const search = searchParams.get("search") ?? undefined;
  const sort = searchParams.get("sort") ?? undefined;
  const orderRaw = searchParams.get("order");

  const params: ListParams = {
    page,
    pageSize: pageSizeRaw ? Number(pageSizeRaw) : undefined,
    search,
    sort,
    order: orderRaw === "asc" || orderRaw === "desc" ? orderRaw : undefined,
  };

  function setParams(partial: Partial<ListParams>) {
    const next = new URLSearchParams(searchParams.toString());

    // Changing what's shown (search/sort/order) invalidates the current
    // page number's meaning, so it always resets to 1 — unless the caller
    // is explicitly setting page itself (e.g. clicking "next page").
    const changesFilters =
      "search" in partial || "sort" in partial || "order" in partial;
    const nextPage = "page" in partial ? partial.page : changesFilters ? 1 : page;

    const merged: ListParams = {
      page: nextPage ?? 1,
      pageSize: "pageSize" in partial ? partial.pageSize : params.pageSize,
      search: "search" in partial ? partial.search : params.search,
      sort: "sort" in partial ? partial.sort : params.sort,
      order: "order" in partial ? partial.order : params.order,
    };

    next.set("page", String(merged.page));
    setOrDelete(next, "pageSize", merged.pageSize?.toString());
    setOrDelete(next, "search", merged.search);
    setOrDelete(next, "sort", merged.sort);
    setOrDelete(next, "order", merged.order);

    router.replace(`${pathname}?${next.toString()}`);
  }

  return { params, setParams };
}

function setOrDelete(params: URLSearchParams, key: string, value: string | undefined) {
  if (value === undefined || value === "") {
    params.delete(key);
  } else {
    params.set(key, value);
  }
}
