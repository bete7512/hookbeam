// safeNext keeps a return address inside the console: only a same-origin path is followed.
export function safeNext(raw: string | null, fallback = "/") {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//")) return fallback;
  return raw;
}
