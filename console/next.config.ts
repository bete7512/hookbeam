import createMDX from "@next/mdx";
import type { NextConfig } from "next";
import remarkGfm from "remark-gfm";

// The console is served behind deploy/proxy (one origin with the API), so it never needs to know the API host.
const nextConfig: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  // The documentation pages under src/app/docs are MDX.
  pageExtensions: ["ts", "tsx", "mdx"],
  // The container image (console/Dockerfile) runs the standalone server; `next start` keeps working without it.
  output: process.env.NEXT_STANDALONE ? "standalone" : undefined,
};

// GitHub-flavoured Markdown gives the documentation its tables.
const withMDX = createMDX({ options: { remarkPlugins: [remarkGfm] } });

export default withMDX(nextConfig);
