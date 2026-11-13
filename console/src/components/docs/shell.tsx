"use client";

import { ArrowLeft, ArrowRight } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { Logo } from "@/components/brand";
import { docLinks, docsNav, guideLinks, type DocLink } from "@/components/docs/nav";
import { ThemeToggle } from "@/components/theme-toggle";
import { buttonVariants } from "@/components/ui/button";
import { repoURL } from "@/lib/repo";
import { cn } from "@/lib/utils";

// The top bar uses plain anchors: the API reference brings its own stylesheet, so entering or leaving it is a
// full navigation and nothing of it lingers on the guide.
export function DocsTopBar() {
  const path = usePathname();
  const onAPI = path.startsWith("/docs/api");
  const tab = (active: boolean) =>
    cn("rounded-md px-3 py-1.5 transition-colors", active ? "bg-secondary text-foreground" : "text-muted-foreground hover:text-foreground");
  return (
    <header className="sticky top-0 z-40 border-b bg-background/85 backdrop-blur supports-[backdrop-filter]:bg-background/70">
      <div className="mx-auto flex h-14 max-w-[90rem] items-center gap-3 px-4 lg:px-8">
        {/* eslint-disable-next-line @next/next/no-html-link-for-pages -- a full navigation drops the API reference's stylesheet */}
        <a href="/" className="inline-flex items-center gap-2.5 font-semibold tracking-tight" aria-label="Hookbeam home">
          <Logo size={26} className="drop-shadow-[0_0_12px_rgba(124,108,255,0.45)]" />
          <span className="text-[15px]">Hookbeam</span>
        </a>
        <span className="rounded-full border px-2 py-0.5 text-xs text-muted-foreground">Docs</span>
        <nav aria-label="Documentation sections" className="ml-2 hidden items-center gap-1 text-sm sm:flex">
          <a href="/docs" className={tab(!onAPI)} aria-current={!onAPI ? "page" : undefined}>
            Guide
          </a>
          <a href="/docs/api" className={tab(onAPI)} aria-current={onAPI ? "page" : undefined}>
            API reference
          </a>
        </nav>
        <div className="ml-auto flex items-center gap-1.5">
          <a href={repoURL} rel="noreferrer" className="hidden rounded-md px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground md:inline-block">
            GitHub
          </a>
          <ThemeToggle />
          <a href="/overview" className={buttonVariants({ size: "sm" })}>
            Open console
          </a>
        </div>
      </div>
    </header>
  );
}

function NavLink({ link, active, onNavigate }: { link: DocLink; active: boolean; onNavigate?: () => void }) {
  const className = cn(
    "block rounded-md px-2.5 py-1.5 transition-colors",
    active ? "bg-sidebar-accent font-medium text-foreground" : "text-muted-foreground hover:bg-secondary hover:text-foreground",
  );
  if (link.fullPage) {
    return (
      <a href={link.href} className={className}>
        {link.title}
      </a>
    );
  }
  return (
    <Link href={link.href} className={className} aria-current={active ? "page" : undefined} onClick={onNavigate}>
      {link.title}
    </Link>
  );
}

export function DocsSidebar({ onNavigate }: { onNavigate?: () => void }) {
  const path = usePathname();
  return (
    <nav aria-label="Documentation" className="flex flex-col gap-6 text-sm">
      {docsNav.map((section) => (
        <div key={section.title}>
          <p className="mb-1.5 px-2.5 text-xs font-medium uppercase tracking-wider text-muted-foreground">{section.title}</p>
          <ul className="flex flex-col gap-0.5">
            {section.links.map((link) => (
              <li key={link.href}>
                <NavLink link={link} active={path === link.href} onNavigate={onNavigate} />
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  );
}

// ForwardReferenceLinks keeps the API reference's old links working. Before the guide existed, /docs was the
// reference, so bookmarks and pages point at its anchors (/docs#tag/me, #model/Error, #description/…). The anchor
// never reaches the server, so the forward happens here, to the same anchor under /docs/api.
export function ForwardReferenceLinks() {
  useEffect(() => {
    const { pathname, hash } = window.location;
    if (pathname === "/docs" && /^#(tag|model|description|operation|webhook)\//.test(hash)) {
      window.location.replace(`/docs/api${hash}`);
    }
  }, []);
  return null;
}

// DocsMobileNav is the sidebar folded into a disclosure under the large breakpoint.
export function DocsMobileNav() {
  const path = usePathname();
  const ref = useRef<HTMLDetailsElement>(null);
  const current = docLinks.find((l) => l.href === path);
  const close = () => {
    if (ref.current) ref.current.open = false;
  };
  return (
    <details ref={ref} className="mb-8 rounded-lg border bg-card lg:hidden">
      <summary className="cursor-pointer px-4 py-3 text-sm font-medium">
        <span className="text-muted-foreground">Documentation: </span>
        {current?.title ?? "contents"}
      </summary>
      <div className="border-t px-2 py-4">
        <DocsSidebar onNavigate={close} />
      </div>
    </details>
  );
}

interface TocItem {
  id: string;
  title: string;
  level: 2 | 3;
}

// DocsToc lists the page's headings and follows the reader's scroll position.
export function DocsToc() {
  const path = usePathname();
  const [items, setItems] = useState<TocItem[]>([]);
  const [active, setActive] = useState("");

  useEffect(() => {
    const headings = Array.from(document.querySelectorAll<HTMLElement>("article h2[id], article h3[id]"));
    setItems(headings.map((h) => ({ id: h.id, title: h.dataset.toc ?? h.textContent ?? "", level: h.tagName === "H3" ? 3 : 2 })));
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible[0]) setActive(visible[0].target.id);
      },
      { rootMargin: "-64px 0px -70% 0px" },
    );
    headings.forEach((h) => observer.observe(h));
    return () => observer.disconnect();
  }, [path]);

  if (items.length === 0) return null;
  return (
    <nav aria-label="On this page" className="text-sm">
      <p className="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">On this page</p>
      <ul className="flex flex-col gap-1 border-l">
        {items.map((item) => (
          <li key={item.id}>
            <a
              href={`#${item.id}`}
              className={cn(
                "-ml-px block border-l py-0.5 transition-colors",
                item.level === 3 ? "pl-6" : "pl-3",
                active === item.id ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {item.title}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  );
}

// DocsPager links the previous and the next page in reading order.
export function DocsPager() {
  const path = usePathname();
  const i = guideLinks.findIndex((l) => l.href === path);
  if (i < 0) return null;
  const prev = guideLinks[i - 1];
  const next = guideLinks[i + 1];
  return (
    <nav aria-label="Previous and next page" className="mx-auto mt-16 grid max-w-[52rem] gap-3 border-t pt-8 sm:grid-cols-2">
      {prev ? (
        <Link href={prev.href} className="group rounded-lg border p-4 transition-colors hover:border-primary/50">
          <span className="flex items-center gap-1 text-xs text-muted-foreground">
            <ArrowLeft aria-hidden className="size-3.5" />
            Previous
          </span>
          <span className="mt-1 block font-medium">{prev.title}</span>
        </Link>
      ) : (
        <span />
      )}
      {next && (
        <Link href={next.href} className="group rounded-lg border p-4 text-right transition-colors hover:border-primary/50">
          <span className="flex items-center justify-end gap-1 text-xs text-muted-foreground">
            Next
            <ArrowRight aria-hidden className="size-3.5" />
          </span>
          <span className="mt-1 block font-medium">{next.title}</span>
        </Link>
      )}
    </nav>
  );
}
