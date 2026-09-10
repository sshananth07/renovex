const formatter = new Intl.DateTimeFormat("en-MY", {
  timeZone: "Asia/Kuala_Lumpur",
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
});

export function formatDate(iso: string): string {
  return formatter.format(new Date(iso));
}

/**
 * Converts a `datetime-local` input value (e.g. "2026-08-20T17:00", no
 * timezone) into an RFC3339 timestamp, interpreting it in the browser's
 * local timezone — the same instant, just serialized with an explicit
 * offset/Z as the backend's strict RFC3339 parser requires. Returns null for
 * blank/unparseable input instead of throwing, so callers can surface a
 * validation message rather than crash on a partially-filled form.
 */
export function toRFC3339(localDateTime: string): string | null {
  if (!localDateTime) return null;
  const date = new Date(localDateTime);
  if (Number.isNaN(date.getTime())) return null;
  return date.toISOString();
}
