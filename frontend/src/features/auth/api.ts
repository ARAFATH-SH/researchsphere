import { type ApiError, call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type { ApiError, Result };

export type RegisterRequest = components["schemas"]["RegisterRequest"];
export type LoginRequest = components["schemas"]["LoginRequest"];
export type CurrentUser = components["schemas"]["CurrentUser"];
export type ChangePasswordRequest = components["schemas"]["ChangePasswordRequest"];
export type ProjectRef = components["schemas"]["ProjectRef"];
export type Role = components["schemas"]["Role"];
export type AdminUser = components["schemas"]["AdminUser"];
export type UserPage = components["schemas"]["UserPage"];

// register creates an account and sends a verification link (FR-1.1 to FR-1.3).
export async function register(body: RegisterRequest): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/register", { body }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// verifyEmail spends the token from a verification link (FR-1.3). Aborting signal ends
// the request with network_error.
export async function verifyEmail(token: string, signal?: AbortSignal): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/verify-email", { body: { token }, signal }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// resendVerification asks for a new link. The server answers the same for every
// well-formed address (FR-1.5).
export async function resendVerification(email: string): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/resend-verification", { body: { email } }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// requestPasswordReset asks for a reset link. The server answers the same whether or
// not the address belongs to an account (FR-2.4).
export async function requestPasswordReset(email: string): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/password-reset/request", { body: { email } }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// confirmPasswordReset spends the token from a reset link and sets the new password. The
// server ends every session of the account (FR-2.3, FR-2.5).
export async function confirmPasswordReset(token: string, newPassword: string): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/password-reset/confirm", { body: { token, newPassword } }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// changePassword sets a new password for the signed-in user. The server ends every other
// session of the account; this device stays signed in (FR-2.6, FR-2.5).
export async function changePassword(body: ChangePasswordRequest): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/password", { body }));
  return res.ok ? { ok: true, data: undefined } : res;
}

// DeleteAccountResult is a Result that, for owns_shared_project, lists the projects that
// block the deletion (FR-2.9).
export type DeleteAccountResult = { ok: true; data: undefined } | ({ ok: false; projects?: ProjectRef[] } & ApiError);

// deleteAccount deletes the signed-in user's account; the server clears the session
// cookies (FR-2.8). It is refused while the user owns a project that has other members.
export async function deleteAccount(password: string): Promise<DeleteAccountResult> {
  let projects: ProjectRef[] | undefined;
  const res = await call(async () => {
    const reply = await api.POST("/account/delete", { body: { password } });
    if (reply.error && "projects" in reply.error) projects = reply.error.projects;
    return reply;
  });
  if (res.ok) return { ok: true, data: undefined };
  return projects ? { ...res, projects } : res;
}

// withUser turns a successful reply that must carry the user into a Result.
function withUser(res: Result<CurrentUser | undefined>): Result<CurrentUser> {
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// login signs a user in; the server sets the session cookies (FR-2.1). An unverified
// address is refused with email_not_verified (FR-1.4), a suspended account with
// account_suspended (FR-3.4).
export async function login(body: LoginRequest): Promise<Result<CurrentUser>> {
  return withUser(await call(() => api.POST("/auth/login", { body })));
}

// getCurrentUser returns the signed-in user, or an error when there is no session.
export async function getCurrentUser(): Promise<Result<CurrentUser>> {
  return withUser(await call(() => api.GET("/auth/me")));
}

// logout ends the session on this device (FR-2.2).
export async function logout(): Promise<Result<undefined>> {
  const res = await call(() => api.POST("/auth/logout"));
  return res.ok ? { ok: true, data: undefined } : res;
}

// listUsers returns one page of users for an administrator (FR-15.3). The server
// refuses everyone else with 403 forbidden (FR-3.3) and caps the page size at 50.
export async function listUsers(page: number, pageSize: number): Promise<Result<UserPage>> {
  const res = await call(() => api.GET("/admin/users", { params: { query: { page, pageSize } } }));
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}
