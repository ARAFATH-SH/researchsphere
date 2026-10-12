// Checks run in the browser before a request, so the user sees a message at once. They
// mirror the server's rules in backend/internal/auth/usecase/validate.go; the server
// still checks everything (NFR-8.1).

export const MAX_FULL_NAME_LENGTH = 200;
export const MAX_EMAIL_LENGTH = 254;
export const MIN_PASSWORD_LENGTH = 8;
export const MAX_PASSWORD_LENGTH = 128;

// confirmPassword is checked only in the browser; the API never receives it.
export type RegisterField = "fullName" | "email" | "password" | "confirmPassword";
export type RegisterValues = Record<RegisterField, string>;
export type FieldErrors = Partial<Record<RegisterField, string>>;

export const messages = {
  fullNameRequired: "Enter your full name.",
  fullNameTooLong: `Your full name can be at most ${MAX_FULL_NAME_LENGTH} characters.`,
  emailInvalid: "Enter a valid email address, such as name@example.org.",
  passwordTooShort: `The password must be at least ${MIN_PASSWORD_LENGTH} characters long.`,
  passwordTooLong: `The password must be at most ${MAX_PASSWORD_LENGTH} characters long.`,
  passwordMismatch: "Passwords do not match.",
  emailRequired: "Enter your email address.",
  passwordRequired: "Enter your password.",
};

// length counts characters (code points), as the server does, not UTF-16 units.
const length = (s: string) => [...s].length;

// A light check only: one @ with text on both sides and no spaces. The server decides.
const emailPattern = /^[^\s@]+@[^\s@]+$/;

export function validateEmail(email: string): string | undefined {
  const value = email.trim();
  if (value.length > MAX_EMAIL_LENGTH || !emailPattern.test(value)) return messages.emailInvalid;
  return undefined;
}

export function validateRegistration(values: RegisterValues): FieldErrors {
  const errors: FieldErrors = {};
  const name = values.fullName.trim();
  if (name === "") errors.fullName = messages.fullNameRequired;
  else if (length(name) > MAX_FULL_NAME_LENGTH) errors.fullName = messages.fullNameTooLong;

  const emailError = validateEmail(values.email);
  if (emailError) errors.email = emailError;

  const n = length(values.password);
  if (n < MIN_PASSWORD_LENGTH) errors.password = messages.passwordTooShort;
  else if (n > MAX_PASSWORD_LENGTH) errors.password = messages.passwordTooLong;

  if (values.confirmPassword !== values.password) errors.confirmPassword = messages.passwordMismatch;
  return errors;
}

export type LoginField = "email" | "password";
export type LoginValues = Record<LoginField, string>;
export type LoginErrors = Partial<Record<LoginField, string>>;

// validateLogin checks only that both fields are filled in and the address looks like
// one. Password length is not checked: a wrong password is the server's answer to give.
export function validateLogin(values: LoginValues): LoginErrors {
  const errors: LoginErrors = {};
  if (values.email.trim() === "") errors.email = messages.emailRequired;
  else {
    const emailError = validateEmail(values.email);
    if (emailError) errors.email = emailError;
  }
  if (values.password === "") errors.password = messages.passwordRequired;
  return errors;
}

// fieldForCode says which field a server error code belongs to (docs/openapi.yaml).
export function fieldForCode(code: string): RegisterField | undefined {
  switch (code) {
    case "invalid_full_name":
      return "fullName";
    case "invalid_email":
    case "email_taken":
      return "email";
    case "password_too_short":
    case "password_too_long":
      return "password";
  }
  return undefined;
}
