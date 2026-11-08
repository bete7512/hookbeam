"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useState } from "react";

import { login, signup } from "@/api/auth";
import { asApiError } from "@/api/errors";
import { Field, FormError } from "@/components/form";
import { Button } from "@/components/ui/button";
import { safeNext } from "@/lib/next-url";

export function SignupForm() {
  const queryClient = useQueryClient();
  const params = useSearchParams();
  const next = safeNext(params.get("next"), "/account");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const register = useMutation({
    mutationFn: async () => {
      await signup({ name, email, password });
      await login({ email, password });
    },
    onSuccess: () => {
      queryClient.clear();
      window.location.assign(next);
    },
  });
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Create your account</h1>
        <p className="text-sm text-muted-foreground">A workspace named after you is created with it.</p>
      </header>
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          register.mutate();
        }}
      >
        <Field id="name" label="Name" autoComplete="name" required value={name} onChange={(e) => setName(e.target.value)} />
        <Field id="email" label="Email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
        <Field id="password" label="Password" type="password" autoComplete="new-password" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} hint="At least 8 characters." />
        <FormError message={register.isError ? asApiError(register.error).message : null} />
        <Button type="submit" disabled={register.isPending}>
          Create account
        </Button>
      </form>
      <p className="text-sm text-muted-foreground">
        Already have one?{" "}
        <Link href="/login" className="underline">
          Sign in
        </Link>
      </p>
    </div>
  );
}
