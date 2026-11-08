import { EventDetail } from "@/components/explorer/event-detail";

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <EventDetail id={Number(id)} />;
}
