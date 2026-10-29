// In production the UI is exported as static files and embedded in the Go binary.
// In `next dev` we skip the export and proxy /api to the Go backend instead.
const backend = process.env.BACKEND_URL || "http://127.0.0.1:8080";

/** @type {(phase: string) => import('next').NextConfig} */
export default function config(phase) {
  const common = { trailingSlash: true, images: { unoptimized: true } };

  if (phase === "phase-development-server") {
    return {
      ...common,
      async rewrites() {
        return [{ source: "/api/:path*", destination: `${backend}/api/:path*` }];
      },
    };
  }

  return { ...common, output: "export" };
}
