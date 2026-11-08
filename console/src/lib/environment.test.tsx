import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { EnvironmentBadge } from "./environment";

const base = { workspaceId: "ws_1", createdBy: "u", createdAt: "2026-11-08T00:00:00Z" };

describe("EnvironmentBadge", () => {
  it("says production in words, with the loud treatment", () => {
    render(<EnvironmentBadge environment={{ id: 2, name: "live", kind: "production", ...base }} />);
    const badge = screen.getByTestId("environment-badge");
    expect(badge).toHaveTextContent("Production · live");
    expect(badge).toHaveAttribute("data-kind", "production");
    expect(badge.className).toContain("bg-destructive");
  });
  it("keeps development quiet", () => {
    render(<EnvironmentBadge environment={{ id: 1, name: "dev", kind: "development", ...base }} />);
    const badge = screen.getByTestId("environment-badge");
    expect(badge).toHaveTextContent("Development · dev");
    expect(badge.className).not.toContain("bg-destructive");
  });
  it("names the absence of an environment", () => {
    render(<EnvironmentBadge environment={null} />);
    expect(screen.getByTestId("environment-badge")).toHaveTextContent("no environment");
  });
});
