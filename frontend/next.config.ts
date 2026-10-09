import type { NextConfig } from "next";

// Where the Go API server listens. Read on the server only: the browser calls
// /api/v1 on this origin and Next.js forwards it, so there are no cross-origin
// requests. `next build` fixes the value at build time.
const apiUrl = (process.env.API_URL ?? "http://localhost:8080").replace(/\/+$/, "");

const nextConfig: NextConfig = {
  cacheComponents: true,
  partialPrefetching: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
  // The check-email page moved from /register/check-email (docs/decisions.md).
  async redirects() {
    return [{ source: "/register/check-email", destination: "/check-email", permanent: false }];
  },
  async rewrites() {
    return [{ source: "/api/v1/:path*", destination: `${apiUrl}/api/v1/:path*` }];
  },
};

export default nextConfig;
