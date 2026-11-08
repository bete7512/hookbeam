import { Suspense } from "react";

import { AcceptInvitation } from "@/components/team/accept-invitation";
import { LoadingState } from "@/components/states";

export default function Page() {
  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-4 py-8">
      <Suspense fallback={<LoadingState />}>
        <AcceptInvitation />
      </Suspense>
    </main>
  );
}
