import { Suspense } from "react";

import { ResetPasswordForm } from "@/components/auth/reset-password-form";
import { LoadingState } from "@/components/states";

export default function Page() {
  return (
    <Suspense fallback={<LoadingState />}>
      <ResetPasswordForm />
    </Suspense>
  );
}
