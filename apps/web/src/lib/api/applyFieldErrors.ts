import type { FieldValues, UseFormSetError, Path } from "react-hook-form";
import type { ApiError } from "./errors";

/**
 * Maps an ApiError's field-scoped errors (Huma ErrorDetail.location values
 * like "body.trade") onto react-hook-form's setError, so a 422 highlights
 * the offending input instead of only showing a generic banner. Returns the
 * detail/title text for any errors that don't correspond to a known form
 * field (e.g. a service-level 422 like "no estimated costs"), so the caller
 * can still show that as a form-level message.
 *
 * `fieldAliases` maps a transport field name (as it appears after the
 * `body.` prefix) to the RHF field name, for forms whose field names diverge
 * from the wire body (e.g. transport `indicativePriceAsOf` -> form `priceAsOf`).
 */
export function applyFieldErrors<T extends FieldValues>(
  error: ApiError,
  setError: UseFormSetError<T>,
  knownFields: readonly string[],
  fieldAliases: Record<string, string> = {},
): string | undefined {
  if (error.kind !== "api") return "The request could not be completed. Check your connection and try again.";

  let mappedAny = false;
  for (const fieldError of error.fieldErrors) {
    const transportField = fieldError.location?.replace(/^body\./, "");
    const field = transportField && (fieldAliases[transportField] ?? transportField);
    if (field && knownFields.includes(field) && fieldError.message) {
      setError(field as Path<T>, { message: fieldError.message });
      mappedAny = true;
    }
  }

  if (mappedAny) return undefined;
  return error.detail ?? error.title ?? "The request could not be completed.";
}
