import type { Metadata } from "next";

import { ApiReference } from "@/components/docs/api-reference";

export const metadata: Metadata = {
  title: "API reference",
  description: "Every operation of Hookbeam's public API, from the OpenAPI document the control plane serves.",
};

export default function ApiReferencePage() {
  return <ApiReference />;
}
