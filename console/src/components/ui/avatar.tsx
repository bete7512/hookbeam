import { cn } from "@/lib/utils";

// Avatar shows a person's initials on the beam gradient; the name is announced, the letters are decoration.
export function Avatar({ name, className, size = 32 }: { name: string; className?: string; size?: number }) {
  const initials = name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("");
  return (
    <span
      role="img"
      aria-label={name}
      className={cn("beam-bg inline-flex shrink-0 items-center justify-center rounded-full text-xs font-semibold text-white", className)}
      style={{ width: size, height: size }}
    >
      {initials || "?"}
    </span>
  );
}
