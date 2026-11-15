"use client";

import { X } from "lucide-react";
import { useState, type KeyboardEvent } from "react";

import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

// TagInput edits a list of short values as chips. Enter, a comma or leaving the field commits what was typed, so
// a value never stays behind in the text box when the form is submitted.
export function TagInput({
  id,
  label,
  values,
  onChange,
  placeholder,
  hint,
  disabled,
  className,
}: {
  id: string;
  label: string;
  values: string[];
  onChange: (next: string[]) => void;
  placeholder?: string;
  hint?: string;
  disabled?: boolean;
  className?: string;
}) {
  const [draft, setDraft] = useState("");
  const hintId = `${id}-hint`;
  const commit = (raw: string) => {
    const added = raw
      .split(/[,\s]+/)
      .map((v) => v.trim())
      .filter((v) => v !== "" && !values.includes(v));
    if (added.length > 0) onChange([...values, ...Array.from(new Set(added))]);
    setDraft("");
  };
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if ((e.key === "Enter" || e.key === ",") && draft.trim() !== "") {
      e.preventDefault();
      commit(draft);
    } else if (e.key === "Backspace" && draft === "" && values.length > 0) {
      onChange(values.slice(0, -1));
    }
  };
  return (
    <div className={cn("flex flex-col gap-1", className)}>
      <Label htmlFor={id}>{label}</Label>
      <div
        className={cn(
          "flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-2 py-1.5 shadow-sm transition-colors focus-within:ring-2 focus-within:ring-ring",
          disabled && "cursor-not-allowed opacity-50",
        )}
      >
        {values.map((v) => (
          <span key={v} className="inline-flex items-center gap-1 rounded-md bg-accent px-2 py-0.5 font-mono text-xs text-accent-foreground" data-testid="tag">
            {v}
            {!disabled && (
              <button type="button" aria-label={`Remove ${v}`} onClick={() => onChange(values.filter((x) => x !== v))} className="rounded-sm opacity-70 transition-opacity hover:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                <X className="h-3 w-3" aria-hidden />
              </button>
            )}
          </span>
        ))}
        <input
          id={id}
          value={draft}
          disabled={disabled}
          placeholder={values.length === 0 ? placeholder : undefined}
          aria-describedby={hint ? hintId : undefined}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={() => commit(draft)}
          className="min-w-24 flex-1 bg-transparent px-1 text-sm outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
        />
      </div>
      {hint && (
        <p id={hintId} className="text-xs text-muted-foreground">
          {hint}
        </p>
      )}
    </div>
  );
}
