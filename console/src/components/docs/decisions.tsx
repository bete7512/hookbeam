import { Fragment } from "react";

import { decisions } from "@/components/docs/decisions-data";
import { sourceURL } from "@/lib/repo";

// title renders a decision's title, setting `backticked` words as code.
function title(text: string) {
  return text.split("`").map((part, i) => (i % 2 === 1 ? <code key={i}>{part}</code> : <Fragment key={i}>{part}</Fragment>));
}

// DecisionList links every architecture decision record in the repository.
export function DecisionList() {
  return (
    <ol className="docs-decisions">
      {decisions.map((d) => (
        <li key={d.number}>
          <a href={sourceURL(`docs/decisions/${d.file}`)} target="_blank" rel="noreferrer">
            <span className="font-mono text-xs text-muted-foreground">{d.number}</span>
            <span>{title(d.title)}</span>
          </a>
        </li>
      ))}
    </ol>
  );
}
