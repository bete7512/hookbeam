import { Suspense } from "react";

import { ApplicationDetail } from "@/components/applications/application-detail";
import { LoadingState } from "@/components/states";

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense fallback={<LoadingState />}>
      <ApplicationDetail id={Number(id)} />
    </Suspense>
  );
}
