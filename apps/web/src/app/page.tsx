import { redirect } from "next/navigation";

// The Contractor app has no standalone landing page; AuthGate on
// (app)/dashboard decides whether the visitor lands there or at /login.
export default function RootPage() {
  redirect("/dashboard");
}
