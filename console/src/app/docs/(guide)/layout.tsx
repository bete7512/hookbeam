import "@xyflow/react/dist/style.css";

import { DocsMobileNav, DocsPager, DocsSidebar, DocsToc } from "@/components/docs/shell";

// The guide: navigation on the left, the page in the middle, its headings on the right.
export default function GuideLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <div className="mx-auto max-w-[90rem] px-4 lg:px-8">
      <div className="lg:grid lg:grid-cols-[14rem_minmax(0,1fr)] lg:gap-10 xl:grid-cols-[14rem_minmax(0,1fr)_13rem]">
        <aside className="sticky top-14 hidden h-[calc(100vh-3.5rem)] overflow-y-auto py-8 lg:block">
          <DocsSidebar />
        </aside>
        <main className="min-w-0 py-8">
          <DocsMobileNav />
          <article className="docs-prose mx-auto max-w-[52rem]">{children}</article>
          <DocsPager />
        </main>
        <aside className="sticky top-14 hidden h-[calc(100vh-3.5rem)] overflow-y-auto py-8 xl:block">
          <DocsToc />
        </aside>
      </div>
    </div>
  );
}
