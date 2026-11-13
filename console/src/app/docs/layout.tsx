import type { Metadata } from "next";

import { DocsTopBar, ForwardReferenceLinks } from "@/components/docs/shell";

export const metadata: Metadata = {
  title: { default: "Documentation", template: "%s · Hookbeam docs" },
  description: "How Hookbeam delivers webhooks: the problem, the architecture, the data model and the API.",
};

export default function DocsLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <div className="min-h-screen">
      <ForwardReferenceLinks />
      <DocsTopBar />
      {children}
    </div>
  );
}
