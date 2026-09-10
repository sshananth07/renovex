const FALLBACK = "/dashboard";

export function parseSafeReturnTo(value: string | null): string {
  if (!value) {
    return FALLBACK;
  }
  if (!value.startsWith("/") || value.startsWith("//") || value.includes("://")) {
    return FALLBACK;
  }
  return value;
}
