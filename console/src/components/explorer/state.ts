// stateTone maps a delivery state to a badge tone; the state word itself is always shown.
export function stateTone(state: string): "good" | "bad" | "warn" | "neutral" {
  switch (state) {
    case "succeeded":
      return "good";
    case "failed":
      return "bad";
    case "cancelled":
      return "neutral";
    default:
      return "warn";
  }
}
