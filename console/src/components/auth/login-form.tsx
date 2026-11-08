"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useState } from "react";

import { login, oauthStartUrl } from "@/api/auth";
import { asApiError } from "@/api/errors";
import { useProviders } from "@/api/queries";
import { Field, FormError } from "@/components/form";
import { Button } from "@/components/ui/button";
import { safeNext } from "@/lib/next-url";

export function LoginForm() {
  const params = useSearchParams();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const providers = useProviders();
  const next = safeNext(params.get("next"));
  const signIn = useMutation({
    mutationFn: () => login({ email, password }),
    onSuccess: () => {
      queryClient.clear();
      // A full navigation: the client router may hold a prefetched redirect from before the cookies existed.
      window.location.assign(next);
    },
  });
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Sign in</h1>
      </header>
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          signIn.mutate();
        }}
      >
        <Field id="email" label="Email" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
        <Field id="password" label="Password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        <FormError message={signIn.isError ? asApiError(signIn.error).message : null} />
        <Button type="submit" disabled={signIn.isPending}>
          Sign in
        </Button>
      </form>
      {providers.data?.some((p) => p.name === "google") && (
        <Button variant="outline" asChild>
          <a href={oauthStartUrl("google")}>Continue with Google</a>
        </Button>
      )}
      <p className="text-sm text-muted-foreground">
        <Link href="/forgot-password" className="underline">
          Forgot your password?
        </Link>{" "}
        · New here?{" "}
        <Link href="/signup" className="underline">
          Create an account
        </Link>
      </p>
    </div>
  );
}

// safeNext keeps redirects inside the console.
