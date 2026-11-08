import { Suspense } from "react";

import { Notifications } from "@/components/notifications/notifications";
import { LoadingState } from "@/components/states";

export default function Page() {
  return (
    <Suspense fallback={<LoadingState />}>
      <Notifications />
    </Suspense>
  );
}
