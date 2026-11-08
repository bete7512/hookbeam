import type { NextConfig } from "next";

// The console is served behind deploy/proxy (one origin with the API), so it never needs to know the API host.
const nextConfig: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
};

export default nextConfig;
