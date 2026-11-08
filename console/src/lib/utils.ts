import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

// cn merges Tailwind class lists the way shadcn/ui components expect.
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
