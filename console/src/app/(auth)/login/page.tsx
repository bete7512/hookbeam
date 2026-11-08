import { Suspense } from "react";

import { LoginForm } from "@/components/auth/login-form";
import { LoadingState } from "@/components/states";

export default function Page() {
  return (
    <Suspense fallback={<LoadingState />}>
      <LoginForm />
    </Suspense>
  );
}
