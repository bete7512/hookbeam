"use client";

import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useState } from "react";

import { resetPassword } from "@/api/auth";
import { asApiError } from "@/api/errors";
import { Field, FormError, FormNotice } from "@/components/form";
import { Button } from "@/components/ui/button";

export function ResetPasswordForm() {
  const params = useSearchParams();
  const token = params.get("token") ?? "";
  const [password, setPassword] = useState("");
  const reset = useMutation({ mutationFn: () => resetPassword(token, password) });
  if (!token) {
    return (
      <div role="alert" className="flex flex-col gap-3">
        <h1 className="text-2xl font-semibold">This link is incomplete</h1>
        <p className="text-sm text-muted-foreground">Open the link from the email again, or request a new one.</p>
        <Link href="/forgot-password" className="underline">
          Request a new link
        </Link>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Choose a new password</h1>
      </header>
      {reset.isSuccess ? (
        <div className="flex flex-col gap-3">
          <FormNotice message="Your password was changed. Every other session was signed out." />
          <Button asChild>
            <Link href="/login">Sign in</Link>
          </Button>
        </div>
      ) : (
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            reset.mutate();
          }}
        >
          <Field id="password" label="New password" type="password" autoComplete="new-password" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} hint="At least 8 characters." />
          <FormError message={reset.isError ? asApiError(reset.error).message : null} />
          <Button type="submit" disabled={reset.isPending}>
            Set password
          </Button>
        </form>
      )}
    </div>
  );
}
