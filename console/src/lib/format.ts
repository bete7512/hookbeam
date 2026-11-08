// Small formatters for the console's tables and tiles.

export function ago(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return "never";
  const seconds = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours}h ago`;
  return `${Math.round(hours / 24)}d ago`;
}

export function when(iso: string | null | undefined): string {
  return iso ? new Date(iso).toLocaleString() : "—";
}

// duration writes a number of seconds the way a person reads retention: days when whole, otherwise hours.
export function duration(seconds: number): string {
  if (seconds % 86400 === 0) {
    const days = seconds / 86400;
    return `${days} day${days === 1 ? "" : "s"}`;
  }
  if (seconds % 3600 === 0) {
    const hours = seconds / 3600;
    return `${hours} hour${hours === 1 ? "" : "s"}`;
  }
  return `${Math.round(seconds / 60)} minutes`;
}

export function shortId(id: string, keep = 10): string {
  return id.length <= keep ? id : `${id.slice(0, keep)}…`;
}
