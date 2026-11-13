"use client";

import "@scalar/api-reference-react/style.css";

import { ApiReferenceReact } from "@scalar/api-reference-react";
import { useEffect, useState } from "react";

import { LoadingState } from "@/components/states";
import { useTheme } from "@/lib/theme";

// Scalar's hosted-product features stay off in a self-hosted reference: its AI agent, its MCP generator and the
// toolbar that offers them are Scalar's services, outside this deployment. These options exist at runtime but not
// in the configuration type Scalar publishes, so they are spread in rather than written in the literal.
const hostedFeaturesOff = { agent: { disabled: true }, mcp: { disabled: true }, showToolbar: "never" };

// Scalar's colours and fonts become the console's, in both themes.
const customCss = `
.light-mode, .dark-mode {
  --scalar-background-1: var(--background);
  --scalar-background-2: var(--card);
  --scalar-background-3: var(--secondary);
  --scalar-color-1: var(--foreground);
  --scalar-color-2: var(--muted-foreground);
  --scalar-color-3: var(--muted-foreground);
  --scalar-color-accent: var(--primary);
  --scalar-background-accent: color-mix(in oklch, var(--primary) 14%, transparent);
  --scalar-border-color: var(--border);
  --scalar-font: var(--font-sans);
  --scalar-font-code: var(--font-mono);
}`;

// ApiReference renders the control plane's OpenAPI document, goauth's routes included, with Scalar. The document
// comes from /openapi.json on the same origin, so it is always the running API's, and "Test request" calls this
// origin rather than the development server the document names.
export function ApiReference() {
  const theme = useTheme();
  const [origin, setOrigin] = useState<string | null>(null);
  useEffect(() => setOrigin(window.location.origin), []);
  if (!origin) return <LoadingState label="Loading the API reference" />;
  return (
    <ApiReferenceReact
      configuration={{
        url: "/openapi.json",
        servers: [{ url: origin, description: "This Hookbeam" }],
        forceDarkModeState: theme,
        hideDarkModeToggle: true,
        withDefaultFonts: false,
        showDeveloperTools: "never",
        telemetry: false,
        customCss,
        ...hostedFeaturesOff,
      }}
    />
  );
}
