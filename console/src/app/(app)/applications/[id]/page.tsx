import { ApplicationDetail } from "@/components/applications/application-detail";

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <ApplicationDetail id={Number(id)} />;
}
