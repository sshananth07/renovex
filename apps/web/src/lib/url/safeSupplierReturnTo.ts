// Validates a `returnTo` deep-link destination carried through Supplier
// Access verification (award outcome notification emails, etc).
//
// The Supplier's session/invitation is the security boundary; `returnTo` is
// only ever a post-auth NAVIGATION target, never a way to reach anything the
// session itself would not already authorize. This helper exists so that
// intent is explicit and testable, rather than a bare `.startsWith(...)`
// check that a future edit could quietly loosen into an open redirect.
//
// A value passes only if, once parsed as a URL relative to an arbitrary
// origin, it resolves to a same-origin, non-protocol-relative path whose
// pathname falls inside /supplier-access/. Anything absolute
// (https://evil.com), protocol-relative (//evil.com), backslash-based
// (/\evil.com), missing its leading slash (supplier-access/...), or outside
// the supplier-access tree (/admin/...) is rejected.
export function safeSupplierReturnTo(value: string | null | undefined): string | null {
  if (!value) return null;
  // Reject anything that isn't an unambiguous path-absolute reference before
  // handing it to the URL parser: backslashes are browser-normalized to
  // forward slashes by some parsers, which is exactly how "/\evil.com" can
  // become protocol-relative.
  if (!value.startsWith("/") || value.startsWith("//") || value.includes("\\")) {
    return null;
  }

  let parsed: URL;
  try {
    parsed = new URL(value, "https://supplier-access.internal.invalid");
  } catch {
    return null;
  }

  if (parsed.origin !== "https://supplier-access.internal.invalid") return null;
  if (!parsed.pathname.startsWith("/supplier-access/")) return null;

  return parsed.pathname + parsed.search + parsed.hash;
}
