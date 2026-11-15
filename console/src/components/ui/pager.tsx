import { Button } from "@/components/ui/button";

// Pager is the previous/next row under an offset-paged table.
export function Pager({ total, limit, offset, onChange, noun }: { total: number; limit: number; offset: number; onChange: (offset: number) => void; noun: string }) {
  const pages = Math.max(1, Math.ceil(total / limit));
  const page = Math.floor(offset / limit) + 1;
  return (
    <div className="flex items-center justify-between gap-3 text-sm">
      <span className="text-muted-foreground">
        {total} {noun} · page {page} of {pages}
      </span>
      <div className="flex gap-2">
        <Button variant="outline" size="sm" disabled={offset === 0} onClick={() => onChange(Math.max(0, offset - limit))}>
          Previous
        </Button>
        <Button variant="outline" size="sm" disabled={offset + limit >= total} onClick={() => onChange(offset + limit)} data-testid="next-page">
          Next
        </Button>
      </div>
    </div>
  );
}
