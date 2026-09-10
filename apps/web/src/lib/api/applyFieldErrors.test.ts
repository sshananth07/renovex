import { describe, expect, it, vi } from "vitest";
import { applyFieldErrors } from "./applyFieldErrors";
import type { ApiError } from "./errors";

describe("applyFieldErrors", () => {
  it("maps a known body.<field> location onto setError and returns no form-level message", () => {
    const setError = vi.fn();
    const error: ApiError = {
      kind: "api",
      status: 422,
      title: "Unprocessable Entity",
      fieldErrors: [{ location: "body.trade", message: "trade is required for ad-hoc labour" }],
    };

    const message = applyFieldErrors(error, setError, ["trade", "workerName"]);

    expect(setError).toHaveBeenCalledWith("trade", { message: "trade is required for ad-hoc labour" });
    expect(message).toBeUndefined();
  });

  it("maps multiple exhaustive field errors in one pass", () => {
    const setError = vi.fn();
    const error: ApiError = {
      kind: "api",
      status: 422,
      fieldErrors: [
        { location: "body.workerName", message: "worker name is required for ad-hoc labour" },
        { location: "body.trade", message: "trade is required for ad-hoc labour" },
        { location: "body.rateAmount", message: "rate is required for ad-hoc labour" },
      ],
    };

    applyFieldErrors(error, setError, ["workerName", "trade", "rate"], { rateAmount: "rate" });

    expect(setError).toHaveBeenCalledTimes(3);
    expect(setError).toHaveBeenCalledWith("workerName", { message: "worker name is required for ad-hoc labour" });
    expect(setError).toHaveBeenCalledWith("trade", { message: "trade is required for ad-hoc labour" });
    expect(setError).toHaveBeenCalledWith("rate", { message: "rate is required for ad-hoc labour" });
  });

  it("uses fieldAliases to map a transport name to a differently-named form field", () => {
    const setError = vi.fn();
    const error: ApiError = {
      kind: "api",
      status: 422,
      fieldErrors: [{ location: "body.indicativePriceAsOf", message: "price as-of date is required" }],
    };

    applyFieldErrors(error, setError, ["priceAsOf"], { indicativePriceAsOf: "priceAsOf" });

    expect(setError).toHaveBeenCalledWith("priceAsOf", { message: "price as-of date is required" });
  });

  it("falls back to a form-level message when no field error is mappable (e.g. a service-level rule)", () => {
    const setError = vi.fn();
    const error: ApiError = {
      kind: "api",
      status: 422,
      title: "Unprocessable Entity",
      detail: "project has no cost items with an estimated amount",
      fieldErrors: [],
    };

    const message = applyFieldErrors(error, setError, ["pricingMode", "pricingRate"]);

    expect(setError).not.toHaveBeenCalled();
    expect(message).toBe("project has no cost items with an estimated amount");
  });

  it("returns a network-error message for non-api errors", () => {
    const setError = vi.fn();
    const message = applyFieldErrors({ kind: "network" }, setError, ["name"]);

    expect(setError).not.toHaveBeenCalled();
    expect(message).toMatch(/could not be completed/i);
  });
});
