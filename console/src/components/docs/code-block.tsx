"use client";

import { Check, Copy } from "lucide-react";
import { isValidElement, useRef, useState, type ComponentProps } from "react";

// CodeBlock is MDX's fenced code: the language in the corner and a copy button.
export function CodeBlock({ children, ...props }: ComponentProps<"pre">) {
  const ref = useRef<HTMLPreElement>(null);
  const [copied, setCopied] = useState(false);
  const className = isValidElement<{ className?: string }>(children) ? (children.props.className ?? "") : "";
  const language = /language-(\w+)/.exec(className)?.[1];

  async function copy() {
    try {
      await navigator.clipboard.writeText(ref.current?.innerText ?? "");
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access can be refused (insecure origin, permissions); the code stays selectable.
    }
  }

  return (
    <div className="docs-code group">
      {language && <span className="docs-code-language">{language}</span>}
      {/* Focusable, so a keyboard can scroll a long line into view. */}
      <pre ref={ref} tabIndex={0} {...props}>
        {children}
      </pre>
      <button
        type="button"
        onClick={copy}
        aria-label={copied ? "Copied" : "Copy code"}
        className="absolute right-2 top-2 rounded-md border bg-card p-1.5 text-muted-foreground opacity-0 transition-opacity hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100"
      >
        {copied ? <Check aria-hidden className="size-3.5" /> : <Copy aria-hidden className="size-3.5" />}
      </button>
    </div>
  );
}
