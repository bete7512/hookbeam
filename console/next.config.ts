import type { NextConfig } from "next";

// The console is served behind deploy/proxy (one origin with the API), so it never needs to know the API host.
const nextConfig: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  // The container image (console/Dockerfile) runs the standalone server; `next start` keeps working without it.
  output: process.env.NEXT_STANDALONE ? "standalone" : undefined,
};

export default nextConfig;
