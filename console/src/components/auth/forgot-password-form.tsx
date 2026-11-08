"use client";

import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { forgotPassword } from "@/api/auth";
import { asApiError } from "@/api/errors";
import { Field, FormError, FormNotice } from "@/components/form";
import { Button } from "@/components/ui/button";

export function ForgotPasswordForm() {
  const [email, setEmail] = useState("");
  const send = useMutation({ mutationFn: () => forgotPassword(email) });
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Reset your password</h1>
        <p className="text-sm text-muted-foreground">We email a link that works once and expires in an hour.</p>
      </header>
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          send.mutate();
        }}
      >
        <Field id="email" label="Email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
        <FormError message={send.isError ? asApiError(send.error).message : null} />
        <FormNotice message={send.isSuccess ? "If an account exists for that address, the email is on its way." : null} />
        <Button type="submit" disabled={send.isPending}>
          Send reset link
        </Button>
      </form>
      <p className="text-sm text-muted-foreground">
        <Link href="/login" className="underline">
          Back to sign in
        </Link>
      </p>
    </div>
  );
}
