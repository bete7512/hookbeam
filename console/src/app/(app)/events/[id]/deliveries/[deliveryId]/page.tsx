import { DeliveryDetail } from "@/components/explorer/delivery-detail";

export default async function Page({ params }: { params: Promise<{ id: string; deliveryId: string }> }) {
  const { id, deliveryId } = await params;
  return <DeliveryDetail messageId={Number(id)} deliveryId={Number(deliveryId)} />;
}
