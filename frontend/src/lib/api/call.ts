import type { components } from "./schema.gen";

// ApiError is the error shape every API error uses (SRS 5.3).
export type ApiError = components["schemas"]["Error"];

export type Result<T> = { ok: true; data: T } | ({ ok: false } & ApiError);

export const networkError: ApiError = {
  code: "network_error",
  message: "Could not reach the server. Check your connection and try again.",
};

export const unexpectedError: ApiError = {
  code: "internal_error",
  message: "Something went wrong on our side. Please try again.",
};

// call runs one request and turns every outcome, including a network failure or a
// reply that is not in the error shape, into a Result. It keeps a refusal's reason and end time
// (account_suspended, messaging_restricted), the field a bad_request names, and the user IDs
// of ineligible_members (FR-16.6).
export async function call<T>(
  request: () => Promise<{ data?: T; error?: ApiError; response: Response }>,
): Promise<Result<T | undefined>> {
  try {
    const { data, error, response } = await request();
    if (response.ok) return { ok: true, data };
    if (error?.code && error.message) {
      const { code, message, reason, until, field, userIds } = error;
      return {
        ok: false,
        code,
        message,
        ...(reason ? { reason } : {}),
        ...(until ? { until } : {}),
        ...(field ? { field } : {}),
        ...(userIds ? { userIds } : {}),
      };
    }
    return { ok: false, ...unexpectedError };
  } catch {
    return { ok: false, ...networkError };
  }
}
