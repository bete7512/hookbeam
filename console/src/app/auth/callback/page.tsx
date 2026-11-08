"use client";

import { useEffect } from "react";

import { LoadingState } from "@/components/states";

// Google sign-in lands here after goauth set the cookies; nothing to read, just continue.
export default function Page() {
  useEffect(() => {
    window.location.replace("/");
  }, []);
  return (
    <main className="p-8">
      <LoadingState label="Signing you in…" />
    </main>
  );
}
