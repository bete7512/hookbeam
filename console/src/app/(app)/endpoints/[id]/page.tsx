import { EndpointDetail } from "@/components/endpoints/endpoint-detail";

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <EndpointDetail id={Number(id)} />;
}
