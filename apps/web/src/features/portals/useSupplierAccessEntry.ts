import { useEffect, useState } from "react";
import {
  createSupplierChallenge,
  getSupplierSession,
  openSupplierAccess,
  resendSupplierChallenge,
  verifySupplierChallenge,
} from "./api";

// Shared Supplier Access authentication bootstrap (M8 §8I).
//
// Both the RFQ offer portal and the award outcome page reach Supplier
// Access the same way: an optional one-time `token` from an email link,
// exchanged via openSupplierAccess() into a short-lived credential, then an
// OTP challenge if the browser doesn't already carry a valid
// `supplier_session` cookie. This hook owns exactly that bootstrap sequence
// and nothing about what a caller does once a session exists — the caller
// decides whether "session established" means "load the RFQ portal" or
// "navigate to the requested outcome".
export type SupplierAccessEntryState =
  | { status: "loading" }
  | { status: "verification-required"; challengeId: string }
  | { status: "error"; message: string }
  | { status: "session-established" };

export function useSupplierAccessEntry(token: string | undefined) {
  const [state, setState] = useState<SupplierAccessEntryState>({ status: "loading" });
  const [verifyError, setVerifyError] = useState("");

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        if (token) {
          await openSupplierAccess(token);
          window.history.replaceState({}, "", window.location.pathname);
          const challenge = await createSupplierChallenge();
          if (alive) setState({ status: "verification-required", challengeId: challenge.challengeId });
          return;
        }
        await getSupplierSession();
        if (alive) setState({ status: "session-established" });
      } catch {
        if (alive) {
          setState({
            status: "error",
            message: "This invitation is invalid, expired, revoked, or cannot be opened.",
          });
        }
      }
    })();
    return () => { alive = false; };
  }, [token]);

  function markSessionEstablished() {
    setState({ status: "session-established" });
  }

  async function verifyCode(code: string) {
    if (state.status !== "verification-required") return;
    setVerifyError("");
    try {
      await verifySupplierChallenge(state.challengeId, code);
    } catch {
      setVerifyError("The verification code is invalid, expired, or no longer current.");
      return;
    }
    markSessionEstablished();
  }

  async function resendCode() {
    if (state.status !== "verification-required") return;
    setVerifyError("");
    try {
      await resendSupplierChallenge(state.challengeId);
    } catch {
      setVerifyError("The verification code could not be resent yet. Please try again shortly.");
    }
  }

  return { state, markSessionEstablished, verifyCode, resendCode, verifyError };
}
