import { api } from "@/lib/api/client";

export type ApiState = "up" | "down";

// checkHealth reports "up" when GET /health answers 200 with status "ok" or "degraded"
// (only the optional AI service is unavailable; everything else works, NFR-10.2).
// A 503, any other error response or a network failure counts as "down".
export async function checkHealth(): Promise<ApiState> {
  try {
    const { data } = await api.GET("/health");
    return data?.status === "ok" || data?.status === "degraded" ? "up" : "down";
  } catch {
    return "down";
  }
}
