"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { Button } from "@/components/ui/button";

// goauth verifies the token on its own route and sends the browser here with ?status=success|error.
export function VerifyEmailResult() {
  const params = useSearchParams();
  const status = params.get("status");
  if (status === "success") {
    return (
      <div className="flex flex-col gap-3">
        <h1 className="text-2xl font-semibold">Email verified</h1>
        <p role="status" className="text-sm text-muted-foreground" data-testid="verify-result">
          Thanks. Alerts about your endpoints can now reach this address.
        </p>
        <Button asChild>
          <Link href="/account">Go to your account</Link>
        </Button>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-3">
      <h1 className="text-2xl font-semibold">That link did not work</h1>
      <p role="alert" className="text-sm text-muted-foreground" data-testid="verify-result">
        Verification links work once and expire. Request a new one from your account page.
      </p>
      <Button variant="outline" asChild>
        <Link href="/account">Go to your account</Link>
      </Button>
    </div>
  );
}
