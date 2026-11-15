"use client";

import { X } from "lucide-react";
import { useEffect, useId, useRef, type ReactNode } from "react";

import { cn } from "@/lib/utils";

// Dialog is a modal over the native <dialog>: the browser traps focus, closes on Escape and dims the page. It is
// mounted only while open, so the form inside starts fresh every time.
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  className,
  ...props
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: ReactNode;
  children: ReactNode;
  className?: string;
  "data-testid"?: string;
}) {
  if (!open) return null;
  return (
    <Modal onClose={onClose} title={title} description={description} className={className} {...props}>
      {children}
    </Modal>
  );
}

function Modal({ onClose, title, description, children, className, ...props }: { onClose: () => void; title: string; description?: ReactNode; children: ReactNode; className?: string }) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descriptionId = useId();
  useEffect(() => {
    const el = ref.current;
    if (!el || el.open) return;
    // jsdom has no showModal; the attribute is enough for the tests to see the content.
    if (typeof el.showModal === "function") el.showModal();
    else el.setAttribute("open", "");
  }, []);
  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      aria-describedby={description ? descriptionId : undefined}
      className={cn(
        "m-auto w-[min(34rem,calc(100vw-2rem))] max-h-[calc(100vh-4rem)] overflow-y-auto rounded-xl border bg-card p-0 text-card-foreground shadow-2xl backdrop:bg-black/60 backdrop:backdrop-blur-sm",
        className,
      )}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onMouseDown={(e) => {
        if (e.target === ref.current) onClose();
      }}
      {...props}
    >
      <div className="flex flex-col gap-4 p-6">
        <div className="flex items-start justify-between gap-4">
          <div className="flex flex-col gap-1">
            <h2 id={titleId} className="text-lg font-semibold leading-tight tracking-tight">
              {title}
            </h2>
            {description && (
              <p id={descriptionId} className="text-sm text-muted-foreground">
                {description}
              </p>
            )}
          </div>
          <button type="button" onClick={onClose} aria-label="Close" className="-m-1 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <X className="h-4 w-4" aria-hidden />
          </button>
        </div>
        {children}
      </div>
    </dialog>
  );
}

// DialogActions is the button row at the bottom of a dialog, primary action last.
export function DialogActions({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center justify-end gap-2 pt-2">{children}</div>;
}
