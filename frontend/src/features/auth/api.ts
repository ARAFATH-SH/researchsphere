import { api, type components } from "@/lib/api/client";

export type RegisterRequest = components["schemas"]["RegisterRequest"];

// ApiError is the error shape every API error uses (SRS 5.3).
export type ApiError = components["schemas"]["Error"];

export type Result<T> = { ok: true; data: T } | ({ ok: false } & ApiError);

const networkError: ApiError = {
  code: "network_error",
  message: "Could not reach the server. Check your connection and try again.",
};

const unexpectedError: ApiError = {
  code: "internal_error",
  message: "Something went wrong on our side. Please try again.",
};

// call runs one request and turns every outcome, including a network failure or a
// reply that is not in the error shape, into a Result.
async function call<T>(
  request: () => Promise<{ data?: T; error?: ApiError; response: Response }>,
): Promise<Result<T | undefined>> {
  try {
    const { data, error, response } = await request();
    if (response.ok) return { ok: true, data };
    if (error?.code && error.message) return { ok: false, code: error.code, message: error.message };
    return { ok: false, ...unexpectedError };
  } catch {
    return { ok: false, ...networkError };
  }
}

// register creates an account and sends a verification link (FR-1.1 to FR-1.3).
export async function register(body: RegisterRequest): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/register", { body }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// verifyEmail spends the token from a verification link (FR-1.3).
export async function verifyEmail(token: string): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/verify-email", { body: { token } }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// resendVerification asks for a new link. The server answers the same for every
// well-formed address (FR-1.5).
export async function resendVerification(email: string): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/resend-verification", { body: { email } }));
  return res.ok ? { ok: true, data: undefined } : res;
}
