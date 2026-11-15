import { Suspense } from "react";

import { EndpointDetail } from "@/components/endpoints/endpoint-detail";
import { LoadingState } from "@/components/states";

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense fallback={<LoadingState />}>
      <EndpointDetail id={Number(id)} />
    </Suspense>
  );
}
