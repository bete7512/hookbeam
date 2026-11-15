import { ChevronRight } from "lucide-react";
import Link from "next/link";
import { Fragment } from "react";

export interface Crumb {
  label: string;
  href?: string;
}

// Breadcrumb is the trail above a detail page's title; the last crumb is the page itself.
export function Breadcrumb({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumb">
      <ol className="flex flex-wrap items-center gap-1.5 text-sm text-muted-foreground">
        {items.map((item, i) => (
          <Fragment key={`${item.label}-${i}`}>
            {i > 0 && (
              <li aria-hidden>
                <ChevronRight className="h-3.5 w-3.5" />
              </li>
            )}
            <li className="min-w-0">
              {item.href ? (
                <Link href={item.href} className="truncate transition-colors hover:text-foreground">
                  {item.label}
                </Link>
              ) : (
                <span aria-current="page" className="truncate text-foreground">
                  {item.label}
                </span>
              )}
            </li>
          </Fragment>
        ))}
      </ol>
    </nav>
  );
}
