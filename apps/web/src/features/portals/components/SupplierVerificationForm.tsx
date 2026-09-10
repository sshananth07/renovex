import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// Shared OTP entry UI for the Supplier Access verification step — used by
// both the RFQ offer portal and the award outcome page, since both reach
// this step through the same useSupplierAccessEntry bootstrap.
export function SupplierVerificationForm({
  code,
  onCodeChange,
  onVerify,
  onResend,
  error,
}: {
  code: string;
  onCodeChange: (value: string) => void;
  onVerify: () => void;
  onResend: () => void;
  error?: string;
}) {
  return (
    <div className="mx-auto max-w-md">
      <section className="surface-card p-6">
        <p className="eyebrow">Email verification</p>
        <h1 className="mt-2 font-heading text-2xl font-semibold">Verify supplier access</h1>
        <p className="mt-2 text-sm text-muted-foreground">A six-digit code was sent to the invitation recipient. Complete verification to establish a scoped Supplier session.</p>
        {error && <p className="mt-4 rounded-lg bg-destructive/5 p-3 text-sm text-destructive">{error}</p>}
        <div className="mt-5 grid gap-2">
          <Label>Verification code</Label>
          <Input
            inputMode="numeric"
            autoComplete="one-time-code"
            maxLength={6}
            value={code}
            onChange={(event) => onCodeChange(event.target.value.replace(/\D/g, ""))}
          />
          <Button className="mt-2" disabled={code.length !== 6} onClick={onVerify}>Verify and continue</Button>
          <Button variant="ghost" onClick={onResend}>Resend code</Button>
        </div>
      </section>
    </div>
  );
}
