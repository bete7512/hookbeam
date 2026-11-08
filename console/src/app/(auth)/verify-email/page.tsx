import { Suspense } from "react";

import { VerifyEmailResult } from "@/components/auth/verify-email-result";
import { LoadingState } from "@/components/states";

export default function Page() {
  return (
    <Suspense fallback={<LoadingState />}>
      <VerifyEmailResult />
    </Suspense>
  );
}
