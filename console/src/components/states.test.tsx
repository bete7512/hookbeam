import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ApiError } from "@/api/errors";

import { EmptyState, QueryState } from "./states";

describe("QueryState", () => {
  it("renders the unavailable state for a 503, never an empty list", () => {
    render(<QueryState error={new ApiError(503, "SERVICE_UNAVAILABLE", "delivery service unavailable")} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Service unavailable");
    expect(screen.queryByText(/nothing here/i)).not.toBeInTheDocument();
  });
  it("renders the unavailable state when the network failed", () => {
    render(<QueryState error={new TypeError("Failed to fetch")} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Service unavailable");
  });
  it("renders permission denied for a 403 with the server's reason", () => {
    render(<QueryState error={new ApiError(403, "FORBIDDEN", "a viewer cannot replay")} />);
    expect(screen.getByRole("alert")).toHaveTextContent("You do not have permission");
    expect(screen.getByRole("alert")).toHaveTextContent("a viewer cannot replay");
  });
  it("offers sign in for a 401", () => {
    render(<QueryState error={new ApiError(401, "UNAUTHORIZED", "authentication required")} />);
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
  });
  it("renders a generic error otherwise", () => {
    render(<QueryState error={new ApiError(500, "INTERNAL_ERROR", "internal server error")} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Something went wrong");
  });
});

describe("EmptyState", () => {
  it("names what is missing", () => {
    render(<EmptyState title="No endpoints yet" hint="Create one to start receiving events." />);
    expect(screen.getByText("No endpoints yet")).toBeInTheDocument();
    expect(screen.getByText("Create one to start receiving events.")).toBeInTheDocument();
  });
});
