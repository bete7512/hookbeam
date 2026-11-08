import { Suspense } from "react";

import { Explorer } from "@/components/explorer/explorer";
import { LoadingState } from "@/components/states";

export default function Page() {
  return (
    <Suspense fallback={<LoadingState />}>
      <Explorer />
    </Suspense>
  );
}
