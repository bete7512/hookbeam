"use client";

import { Lock, Plus, Undo2, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export interface HeaderDraft {
  name: string;
  value: string;
}

export interface HeaderChanges {
  added: HeaderDraft[];
  removed: string[];
}

export const noHeaderChanges: HeaderChanges = { added: [], removed: [] };

// filled keeps the drafts that name a header; a row left blank is not a change.
function filled(drafts: HeaderDraft[]) {
  return drafts.map((d) => ({ name: d.name.trim().toLowerCase(), value: d.value.trim() })).filter((d) => d.name !== "");
}

// headersForCreate is the map a new endpoint is created with, or undefined when there is none.
export function headersForCreate(changes: HeaderChanges): Record<string, string> | undefined {
  const rows = filled(changes.added);
  return rows.length === 0 ? undefined : Object.fromEntries(rows.map((d) => [d.name, d.value]));
}

// headersPatch is the merge an update sends: a value sets a header, null removes it; undefined means no change.
export function headersPatch(changes: HeaderChanges): Record<string, string | null> | undefined {
  const rows = filled(changes.added);
  if (rows.length === 0 && changes.removed.length === 0) return undefined;
  return { ...Object.fromEntries(changes.removed.map((n) => [n, null])), ...Object.fromEntries(rows.map((d) => [d.name, d.value])) };
}

// HeaderEditor edits an endpoint's custom headers. Stored values are never shown: a header can be removed, or
// replaced by adding it again with a new value.
export function HeaderEditor({ names, changes, onChange, disabled }: { names: string[]; changes: HeaderChanges; onChange: (next: HeaderChanges) => void; disabled?: boolean }) {
  const setDraft = (index: number, patch: Partial<HeaderDraft>) => onChange({ ...changes, added: changes.added.map((d, i) => (i === index ? { ...d, ...patch } : d)) });
  return (
    <fieldset className="flex flex-col gap-2" disabled={disabled}>
      <legend className="text-sm font-medium">Custom headers</legend>
      <p className="text-xs text-muted-foreground">
        Sent with every delivery, for example an <code>authorization</code> token your receiver expects. Values are encrypted and never shown again; to change one, add the header again.
      </p>
      {names.length > 0 && (
        <ul className="flex flex-col gap-1.5" data-testid="header-names">
          {names.map((name) => {
            const removed = changes.removed.includes(name);
            return (
              <li key={name} className="flex items-center gap-2 rounded-md border bg-background px-3 py-1.5 text-sm">
                <code className={removed ? "text-muted-foreground line-through" : ""}>{name}</code>
                <span className="inline-flex flex-1 items-center gap-1.5 text-xs text-muted-foreground">
                  <Lock className="h-3 w-3" aria-hidden />
                  {removed ? "removed when you save" : "value hidden"}
                </span>
                {removed ? (
                  <Button type="button" variant="ghost" size="sm" onClick={() => onChange({ ...changes, removed: changes.removed.filter((n) => n !== name) })}>
                    <Undo2 className="h-3.5 w-3.5" aria-hidden />
                    Keep
                  </Button>
                ) : (
                  <Button type="button" variant="ghost" size="sm" onClick={() => onChange({ ...changes, removed: [...changes.removed, name] })} aria-label={`Remove header ${name}`}>
                    <X className="h-3.5 w-3.5" aria-hidden />
                    Remove
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {changes.added.map((draft, i) => (
        <div key={i} className="flex items-center gap-2">
          <Input aria-label={`Header ${i + 1} name`} placeholder="x-api-key" value={draft.name} onChange={(e) => setDraft(i, { name: e.target.value })} className="w-2/5 font-mono text-xs" autoComplete="off" spellCheck={false} />
          <Input aria-label={`Header ${i + 1} value`} placeholder="value" value={draft.value} onChange={(e) => setDraft(i, { value: e.target.value })} className="flex-1 font-mono text-xs" autoComplete="off" spellCheck={false} />
          <Button type="button" variant="ghost" size="sm" onClick={() => onChange({ ...changes, added: changes.added.filter((_, j) => j !== i) })} aria-label={`Discard header ${i + 1}`}>
            <X className="h-3.5 w-3.5" aria-hidden />
          </Button>
        </div>
      ))}
      <div>
        <Button type="button" variant="outline" size="sm" onClick={() => onChange({ ...changes, added: [...changes.added, { name: "", value: "" }] })} data-testid="add-header">
          <Plus className="h-3.5 w-3.5" aria-hidden />
          Add header
        </Button>
      </div>
    </fieldset>
  );
}
