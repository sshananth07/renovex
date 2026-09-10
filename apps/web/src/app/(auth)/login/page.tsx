import Link from "next/link";
import { Suspense } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { LoginForm } from "./LoginForm";

export default function LoginPage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Sign in</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {/* useSearchParams() (for ?returnTo=) requires a Suspense boundary
              since it isn't known at static-prerender time. */}
          <Suspense>
            <LoginForm />
          </Suspense>
          <p className="text-center text-sm text-muted-foreground">
            No account?{" "}
            <Link href="/register" className="font-medium text-foreground underline">
              Register
            </Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
