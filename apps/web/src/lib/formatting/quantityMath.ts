type ParsedDecimal = { value: bigint; scale: number };

const DECIMAL_RE = /^([+-]?)(\d+)(?:\.(\d+))?$/;

function parseDecimal(input: string): ParsedDecimal {
  const text = input.trim();
  const match = DECIMAL_RE.exec(text);
  if (!match) throw new Error(`Invalid decimal quantity: ${input}`);

  const [, sign, whole, fraction = ""] = match;
  const digits = `${whole}${fraction}`.replace(/^0+(?=\d)/, "");
  const unsigned = BigInt(digits || "0");
  return {
    value: sign === "-" ? -unsigned : unsigned,
    scale: fraction.length,
  };
}

function pow10(exp: number): bigint {
  return BigInt(10) ** BigInt(exp);
}

function align(a: ParsedDecimal, b: ParsedDecimal): [bigint, bigint, number] {
  const scale = Math.max(a.scale, b.scale);
  return [
    a.value * pow10(scale - a.scale),
    b.value * pow10(scale - b.scale),
    scale,
  ];
}

function formatDecimal(value: bigint, scale: number): string {
  const zero = BigInt(0);
  if (value === zero) return "0";

  const negative = value < zero;
  let digits = (negative ? -value : value).toString();

  if (scale > 0) {
    digits = digits.padStart(scale + 1, "0");
    const split = digits.length - scale;
    const whole = digits.slice(0, split);
    const fraction = digits.slice(split).replace(/0+$/, "");
    digits = fraction ? `${whole}.${fraction}` : whole;
  }

  digits = digits.replace(/^0+(?=\d)/, "");
  return negative ? `-${digits}` : digits;
}

export function addQuantity(a: string, b: string): string {
  const [left, right, scale] = align(parseDecimal(a), parseDecimal(b));
  return formatDecimal(left + right, scale);
}

export function subtractQuantity(a: string, b: string): string {
  const [left, right, scale] = align(parseDecimal(a), parseDecimal(b));
  return formatDecimal(left - right, scale);
}
