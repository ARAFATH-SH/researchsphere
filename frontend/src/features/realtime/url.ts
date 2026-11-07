// socketUrl is the API's socket. The browser goes to it straight, not through the Next.js rewrite
// (docs/decisions.md). A NEXT_PUBLIC_ value is fixed at `next build`.
export function socketUrl(): string {
  return process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080/api/v1/ws";
}
