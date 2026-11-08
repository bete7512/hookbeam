"use client";

import { Moon, Sun } from "lucide-react";
import { useEffect, useState } from "react";

import { applyTheme, currentTheme, type Theme } from "@/lib/theme";
import { cn } from "@/lib/utils";

// ThemeToggle switches between night and day; the choice is kept in the browser.
export function ThemeToggle({ className }: { className?: string }) {
  const [theme, setTheme] = useState<Theme>("dark");
  useEffect(() => setTheme(currentTheme()), []);
  const next: Theme = theme === "dark" ? "light" : "dark";
  return (
    <button
      type="button"
      className={cn("inline-flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground", className)}
      aria-label={`Switch to ${next} mode`}
      title={`Switch to ${next} mode`}
      onClick={() => {
        applyTheme(next);
        setTheme(next);
      }}
      data-testid="theme-toggle"
    >
      {theme === "dark" ? <Sun className="h-4 w-4" aria-hidden /> : <Moon className="h-4 w-4" aria-hidden />}
    </button>
  );
}
