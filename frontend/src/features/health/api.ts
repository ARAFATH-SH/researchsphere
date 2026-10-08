import { api } from "@/lib/api/client";

export type ApiState = "up" | "down";

// checkHealth reports "up" only when GET /health answers 200 with status "ok".
// A 503, any other error response or a network failure counts as "down".
export async function checkHealth(): Promise<ApiState> {
  try {
    const { data } = await api.GET("/health");
    return data?.status === "ok" ? "up" : "down";
  } catch {
    return "down";
  }
}
