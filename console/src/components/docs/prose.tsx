import { ArrowRight, FileCode2, Info, TriangleAlert } from "lucide-react";
import type { MDXComponents } from "mdx/types";
import Link from "next/link";
import { isValidElement, type ComponentProps, type ReactNode } from "react";

import { CodeBlock } from "@/components/docs/code-block";
import { docLinks } from "@/components/docs/nav";
import { sourceURL } from "@/lib/repo";
import { cn } from "@/lib/utils";

// textOf flattens children to their words, so a heading's anchor follows its text.
export function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children);
  return "";
}

export function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function heading(Tag: "h2" | "h3") {
  return function Heading({ children }: { children?: ReactNode }) {
    const title = textOf(children);
    const id = slugify(title);
    return (
      <Tag id={id} data-toc={title} className="group scroll-mt-24">
        {children}
        <a
          href={`#${id}`}
          aria-label={`Link to ${title}`}
          className="ml-2 font-normal text-muted-foreground no-underline opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        >
          #
        </a>
      </Tag>
    );
  };
}

// A links internal pages through the client router, except the API reference, which loads its own stylesheet and
// is entered and left with a full navigation.
function A({ href = "", children, ...rest }: ComponentProps<"a">) {
  if (href.startsWith("/docs/api") || href.startsWith("#")) {
    return (
      <a href={href} {...rest}>
        {children}
      </a>
    );
  }
  if (href.startsWith("/")) {
    return (
      <Link href={href} {...rest}>
        {children}
      </Link>
    );
  }
  return (
    <a href={href} target="_blank" rel="noreferrer" {...rest}>
      {children}
    </a>
  );
}

function Table(props: ComponentProps<"table">) {
  return (
    <div className="docs-table">
      <table {...props} />
    </div>
  );
}

// Lead is the opening paragraph of a page, set larger. A div, not a p: MDX wraps the text inside in its own
// paragraph, and a paragraph inside a paragraph is invalid HTML that breaks hydration.
export function Lead({ children }: { children: ReactNode }) {
  return <div className="docs-lead">{children}</div>;
}

export function Callout({ title, tone = "note", children }: { title: string; tone?: "note" | "warn"; children: ReactNode }) {
  const Icon = tone === "warn" ? TriangleAlert : Info;
  return (
    <aside className={cn("docs-callout", tone === "warn" ? "docs-callout-warn" : "docs-callout-note")}>
      <p className="docs-callout-title">
        <Icon aria-hidden className="size-4" />
        {title}
      </p>
      <div className="docs-callout-body">{children}</div>
    </aside>
  );
}

// Source links one repository path; docs.test.ts checks that it exists.
export function Source({ path, children }: { path: string; children?: ReactNode }) {
  return (
    <a href={sourceURL(path)} target="_blank" rel="noreferrer" className="docs-source">
      <FileCode2 aria-hidden className="size-3.5" />
      <span>{children ?? path}</span>
    </a>
  );
}

// Sources ends a section with the files that implement it.
export function Sources({ paths }: { paths: string[] }) {
  return (
    <div className="docs-sources">
      <span className="text-muted-foreground">In the code:</span>
      {paths.map((p) => (
        <Source key={p} path={p} />
      ))}
    </div>
  );
}

// NextPages points at other pages of the guide with the summaries the navigation already holds.
export function NextPages({ hrefs }: { hrefs: string[] }) {
  const links = hrefs.map((href) => {
    const link = docLinks.find((l) => l.href === href);
    if (!link) throw new Error(`docs: no page ${href}`);
    return link;
  });
  return (
    <div className="not-prose my-6 grid gap-3 sm:grid-cols-2">
      {links.map((l) => (
        <A key={l.href} href={l.href} className="group rounded-lg border bg-card p-4 no-underline transition-colors hover:border-primary/50">
          <span className="flex items-center justify-between text-sm font-medium text-foreground">
            {l.title}
            <ArrowRight aria-hidden className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
          </span>
          <span className="mt-1 block text-sm text-muted-foreground">{l.summary}</span>
        </A>
      ))}
    </div>
  );
}

export const proseComponents: MDXComponents = {
  h2: heading("h2"),
  h3: heading("h3"),
  a: A,
  table: Table,
  pre: CodeBlock,
};
