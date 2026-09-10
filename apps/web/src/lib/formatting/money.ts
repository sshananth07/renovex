export function formatMoney(amountMinor?: number, currency = "MYR") {
  if (amountMinor === undefined || amountMinor === null) return "—";
  return new Intl.NumberFormat("en-MY", {
    style: "currency",
    currency,
    minimumFractionDigits: 2,
  }).format(amountMinor / 100);
}

export function majorToMinor(value: string) {
  const normalized = value.trim();
  if (!/^\d+(?:\.\d{1,2})?$/.test(normalized)) return null;
  const [whole, fraction = ""] = normalized.split(".");
  const minor = BigInt(whole) * BigInt(100) + BigInt(fraction.padEnd(2, "0"));
  if (minor > BigInt(Number.MAX_SAFE_INTEGER)) return null;
  return Number(minor);
}

// Exact minor-unit integer -> major-unit decimal string, using integer string
// manipulation only (BigInt), never a floating-point division — the inverse
// of majorToMinor. Used anywhere a stored minor-unit amount must pre-populate
// an editable form field as a string.
export function minorToMajorString(amountMinor: number): string {
  const negative = amountMinor < 0;
  const digits = Math.abs(amountMinor).toString().padStart(3, "0");
  const whole = digits.slice(0, -2);
  const fraction = digits.slice(-2);
  return `${negative ? "-" : ""}${whole}.${fraction}`;
}

export function formatBasisPoints(value?: number) {
  if (value === undefined) return "—";
  return `${(value / 100).toFixed(2)}%`;
}
