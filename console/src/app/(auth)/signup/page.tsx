import { Suspense } from "react";

import { SignupForm } from "@/components/auth/signup-form";
import { LoadingState } from "@/components/states";

export default function Page() {
  return (
    <Suspense fallback={<LoadingState />}>
      <SignupForm />
    </Suspense>
  );
}
