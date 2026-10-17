// Checks for contact info (FR-4.7), run before a request. They mirror the server's rules
// in backend/internal/profile/usecase/contact.go; the server still checks everything.

import type { LinkKind, Visibility } from "./api";
import { length } from "./validation";

export const MAX_LINKS = 5;
export const MAX_LINK_URL_LENGTH = 300;

export const visibilityLabels: Record<Visibility, string> = {
  only_me: "Only me",
  logged_in: "All logged-in users",
};

// daysIn is how many days a month can have. February has 29: there is no year to rule
// 29 February out.
export function daysIn(month: number): number {
  return [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1] ?? 31;
}

const monthNames = new Intl.DateTimeFormat("en", { month: "long", timeZone: "UTC" });

// monthName names a month, 1 to 12 ("February").
export function monthName(month: number): string {
  return monthNames.format(new Date(Date.UTC(2000, month - 1, 1)));
}

// birthdayText shows a birthday without a year: "29 February".
export function birthdayText(b: { month: number; day: number }): string {
  return `${b.day} ${monthName(b.month)}`;
}

export const contactMessages = {
  phoneInvalid: "Enter the phone number in international format, such as +8801712345678.",
  birthdayIncomplete: "Choose both a month and a day, or neither.",
  birthdayInvalid: "Choose a day that exists in the chosen month.",
  linkRequired: "Enter the link.",
  linkNotHttps: "Links must start with https://.",
  linkInvalid: "Enter a full link, such as https://example.org.",
  linkTooLong: `A link can be at most ${MAX_LINK_URL_LENGTH} characters.`,
};

// normalizePhone removes spaces and hyphens, as the server does.
export function normalizePhone(phone: string): string {
  return phone.replace(/[\s-]/g, "");
}

const e164 = /^\+[1-9][0-9]{1,14}$/;

export type ContactField = "phone" | "birthday";
export type ContactValues = {
  emailVisibility: Visibility;
  phone: string;
  phoneVisibility: Visibility;
  // month and day are "" when not set.
  month: string;
  day: string;
  birthdayVisibility: Visibility;
};
export type ContactErrors = Partial<Record<ContactField, string>>;

// validateContact checks the phone and the birthday (FR-4.7).
export function validateContact(v: ContactValues): ContactErrors {
  const errors: ContactErrors = {};
  const phone = normalizePhone(v.phone);
  if (phone !== "" && !e164.test(phone)) errors.phone = contactMessages.phoneInvalid;
  if ((v.month === "") !== (v.day === "")) errors.birthday = contactMessages.birthdayIncomplete;
  else if (v.month !== "") {
    const month = Number(v.month);
    const day = Number(v.day);
    if (!(month >= 1 && month <= 12 && day >= 1 && day <= daysIn(month))) errors.birthday = contactMessages.birthdayInvalid;
  }
  return errors;
}

// fieldForContactCode says which field a PUT /profiles/me/contact error code belongs to.
export function fieldForContactCode(code: string): ContactField | undefined {
  if (code === "invalid_phone") return "phone";
  if (code === "invalid_birthday") return "birthday";
  return undefined;
}

// validateLinkURL checks a link before it is added: https, a host, no whitespace.
export function validateLinkURL(url: string): string | undefined {
  const v = url.trim();
  if (v === "") return contactMessages.linkRequired;
  if (!/^https:\/\//i.test(v)) return contactMessages.linkNotHttps;
  if (length(v) > MAX_LINK_URL_LENGTH) return contactMessages.linkTooLong;
  if (/\s/.test(v)) return contactMessages.linkInvalid;
  try {
    if (new URL(v).host === "") return contactMessages.linkInvalid;
  } catch {
    return contactMessages.linkInvalid;
  }
  return undefined;
}

export const linkKinds: readonly LinkKind[] = ["website", "google_scholar", "researchgate", "linkedin", "github", "other"];

// linkLabels name the site a link points to. The names are text only; the icons are our
// own, never the sites' logos (docs/ui-guidelines.md).
export const linkLabels: Record<LinkKind, string> = {
  website: "Website",
  google_scholar: "Google Scholar",
  researchgate: "ResearchGate",
  linkedin: "LinkedIn",
  github: "GitHub",
  other: "Link",
};
