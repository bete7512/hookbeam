import { Suspense } from "react";

import { Audit } from "@/components/audit/audit";
import { LoadingState } from "@/components/states";

export default function Page() {
  return (
    <Suspense fallback={<LoadingState />}>
      <Audit />
    </Suspense>
  );
}
