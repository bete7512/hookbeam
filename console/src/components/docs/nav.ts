// The documentation's table of contents. Every guide page under src/app/docs/(guide) appears here exactly once,
// in reading order, and its title matches the page's heading; docs.test.ts fails on an orphan either way.

export interface DocLink {
  href: string;
  title: string;
  summary: string;
  // fullPage links leave the guide with a full navigation (the API reference brings its own stylesheet).
  fullPage?: boolean;
}

export interface DocSection {
  title: string;
  links: DocLink[];
}

export const docsNav: DocSection[] = [
  {
    title: "Start here",
    links: [
      { href: "/docs", title: "Why Hookbeam", summary: "The problem with sending webhooks yourself, and what Hookbeam guarantees instead." },
      { href: "/docs/concepts", title: "Concepts", summary: "Workspaces, environments, applications, endpoints, messages, deliveries and attempts." },
    ],
  },
  {
    title: "System design",
    links: [
      { href: "/docs/architecture", title: "Architecture", summary: "Two planes, two databases, one contract, and why they are split." },
      { href: "/docs/lifecycle", title: "Life of a message", summary: "One event from the sender's request to the receiver and back to the console, step by step." },
      { href: "/docs/delivery", title: "Delivery engine", summary: "Claims, leases, fairness, the retry policy and the delivery state machine." },
      { href: "/docs/data-model", title: "Data model", summary: "Every table of both databases, its keys and the indexes that carry the design." },
      { href: "/docs/reliability", title: "Reliability", summary: "The outbox, idempotency, crash recovery, backpressure and graceful shutdown." },
      { href: "/docs/security", title: "Security", summary: "Credentials, roles, signing, secrets at rest, the SSRF guard and tenant isolation." },
      { href: "/docs/observability", title: "Observability", summary: "Traces linked across the queue, bounded metrics, alerts and their runbooks." },
    ],
  },
  {
    title: "Build with it",
    links: [
      { href: "/docs/receivers", title: "Receiving webhooks", summary: "Verify the signature, deduplicate, and answer so retries do the right thing." },
      { href: "/docs/run", title: "Run it locally", summary: "The whole product in Docker Compose, and the developer loop." },
      { href: "/docs/api", title: "API reference", summary: "Every operation of the public API, generated from the OpenAPI document.", fullPage: true },
    ],
  },
  {
    title: "Reference",
    links: [{ href: "/docs/decisions", title: "Design decisions", summary: "The architecture decisions behind the system, one record each." }],
  },
];

export const docLinks: DocLink[] = docsNav.flatMap((s) => s.links);

// guideLinks are the pages inside the guide layout, the ones with a previous and a next page.
export const guideLinks: DocLink[] = docLinks.filter((l) => !l.fullPage);
