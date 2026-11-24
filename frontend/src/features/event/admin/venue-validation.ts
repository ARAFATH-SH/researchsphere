import type { Venue, VenueAttendance, VenueInput, VenuePatch, VenueType } from "../api";

// The administrator's venue form (FR-15.1) holds strings, as the controls do; toInput turns them
// into a VenueInput. validate repeats the rules of VenueInput (docs/openapi.yaml) so that most
// mistakes are caught before the request; the server still checks every rule.

export const maxName = 300;
export const maxShortName = 40;
export const maxCity = 100;
export const maxScope = 10_000;
export const maxUrl = 2048;
export const maxFields = 5;

export type VenueForm = {
  name: string;
  shortName: string;
  type: VenueType;
  rolling: boolean;
  fields: string[];
  attendance: VenueAttendance | "";
  city: string;
  country: string;
  startsOn: string;
  endsOn: string;
  timeZone: string;
  submissionDeadline: string;
  abstractDeadline: string;
  notificationAt: string;
  officialUrl: string;
  scope: string;
};

export type FormField = keyof VenueForm;
export type FormErrors = Partial<Record<FormField, string>>;

export const emptyForm: VenueForm = {
  name: "",
  shortName: "",
  type: "conference",
  rolling: false,
  fields: [],
  attendance: "",
  city: "",
  country: "",
  startsOn: "",
  endsOn: "",
  timeZone: "AoE",
  submissionDeadline: "",
  abstractDeadline: "",
  notificationAt: "",
  officialUrl: "https://",
  scope: "",
};

const isEvent = (t: VenueType) => t === "conference" || t === "workshop";

// formOf is the form for an existing venue. Deadlines are the venue's local times.
export function formOf(v: Venue): VenueForm {
  return {
    name: v.name,
    shortName: v.shortName ?? "",
    type: v.type,
    rolling: v.rolling,
    fields: v.fields.map((f) => f.key),
    attendance: v.attendance ?? "",
    city: v.city ?? "",
    country: v.country ?? "",
    startsOn: v.startsOn ?? "",
    endsOn: v.endsOn ?? "",
    timeZone: v.timeZone ?? (v.rolling ? "" : "AoE"),
    submissionDeadline: v.submissionDeadline?.local ?? "",
    abstractDeadline: v.abstractDeadline?.local ?? "",
    notificationAt: v.notificationAt?.local ?? "",
    officialUrl: v.officialUrl,
    scope: v.scope,
  };
}

// shows says which inputs apply to the form as it is: attendance and event dates for events,
// a place for in-person and hybrid events, a rolling switch for journals, and the zone and
// deadlines for every venue that is not rolling.
export function shows(f: VenueForm) {
  const event = isEvent(f.type);
  const rolling = f.type === "journal" && f.rolling;
  return {
    attendance: event,
    place: event && (f.attendance === "in_person" || f.attendance === "hybrid"),
    dates: event,
    rolling: f.type === "journal",
    deadlines: !rolling,
  };
}

const orNull = (s: string) => (s.trim() === "" ? null : s.trim());

// toInput is the request body of the form. Inputs that do not apply are null.
export function toInput(f: VenueForm): VenueInput {
  const s = shows(f);
  return {
    name: f.name.trim(),
    shortName: orNull(f.shortName),
    type: f.type,
    rolling: s.rolling && f.rolling,
    fields: f.fields,
    attendance: s.attendance && f.attendance ? f.attendance : null,
    city: s.place ? orNull(f.city) : null,
    country: s.place ? orNull(f.country.toUpperCase()) : null,
    startsOn: s.dates ? orNull(f.startsOn) : null,
    endsOn: s.dates ? orNull(f.endsOn) : null,
    timeZone: s.deadlines ? orNull(f.timeZone) : null,
    submissionDeadline: s.deadlines ? orNull(f.submissionDeadline) : null,
    abstractDeadline: s.deadlines ? orNull(f.abstractDeadline) : null,
    notificationAt: s.deadlines ? orNull(f.notificationAt) : null,
    officialUrl: f.officialUrl.trim(),
    scope: f.scope.trim(),
  };
}

// toPatch is the PATCH body: only what changed from the venue, null to clear an optional value.
// The local times are compared to the second, so an unchanged deadline is not sent.
export function toPatch(original: Venue, f: VenueForm): VenuePatch {
  const before = toInput(formOf(original));
  const after = toInput(f);
  const patch: Record<string, unknown> = {};
  for (const key of Object.keys(after) as (keyof VenueInput)[]) {
    const a = after[key];
    const b = before[key];
    const same =
      Array.isArray(a) && Array.isArray(b)
        ? a.length === b.length && a.every((x, i) => x === b[i])
        : key.endsWith("Deadline") || key === "notificationAt"
          ? withSeconds(a as string | null) === withSeconds(b as string | null)
          : (a ?? null) === (b ?? null);
    if (!same) patch[key] = a ?? null;
  }
  return patch as VenuePatch;
}

// localTime is a datetime-local value without fractions of a second, which some browsers
// add ("23:59:59.000") and the API refuses.
export function localTime(value: string): string {
  return value.replace(/(T\d{2}:\d{2}:\d{2})\.\d+$/, "$1");
}

// withSeconds writes a local time as YYYY-MM-DDTHH:MM:SS; a date alone means 23:59:59.
export function withSeconds(t: string | null | undefined): string | null {
  if (!t) return null;
  if (/^\d{4}-\d{2}-\d{2}$/.test(t)) return `${t}T23:59:59`;
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(t)) return `${t}:00`;
  return t;
}

const dateRe = /^\d{4}-\d{2}-\d{2}$/;
const localRe = /^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}(:\d{2})?)?$/;
// Control characters other than tab and line feed (carriage returns are folded by the server).
const badControl = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/;

let zones: Set<string> | null = null;
// knownZones is the browser's list of IANA names plus AoE and UTC.
export function knownZones(): string[] {
  let list: string[] = [];
  try {
    list = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.("timeZone") ?? [];
  } catch {
    list = [];
  }
  return ["AoE", "UTC", ...list.filter((z) => z !== "UTC")];
}

// validZone accepts AoE, UTC and a name the browser knows; when it knows none, any Area/City.
export function validZone(zone: string): boolean {
  if (zone === "AoE" || zone === "UTC") return true;
  zones ??= new Set(knownZones());
  return zones.size > 2 ? zones.has(zone) : /^[A-Za-z_]+\/[A-Za-z0-9_+\-/]+$/.test(zone);
}

// urlError says what is wrong with a link, or "" when it is an https link with a host and no
// user name.
export function urlError(raw: string): string {
  const s = raw.trim();
  if (s === "" || s === "https://") return "Enter the venue's https:// link.";
  if (s.length > maxUrl) return `A link may be at most ${maxUrl.toLocaleString("en-US")} characters.`;
  let u: URL;
  try {
    u = new URL(s);
  } catch {
    return "This is not a valid link.";
  }
  if (u.protocol !== "https:") return "The link must start with https://.";
  if (!u.hostname) return "The link needs a host name.";
  if (u.username || u.password) return "The link must not contain a user name or password.";
  return "";
}

// validate returns a message per field that breaks a rule of VenueInput.
export function validate(f: VenueForm): FormErrors {
  const e: FormErrors = {};
  const s = shows(f);
  const name = f.name.trim();
  if (!name) e.name = "Enter the venue's name.";
  else if (name.length > maxName) e.name = `The name may be at most ${maxName} characters.`;
  else if (/[\r\n]/.test(name)) e.name = "The name must be on one line.";
  const short = f.shortName.trim();
  if (short.length > maxShortName) e.shortName = `The short name may be at most ${maxShortName} characters.`;
  else if (/[\r\n]/.test(short)) e.shortName = "The short name must be on one line.";

  if (f.fields.length === 0) e.fields = "Choose at least one research field.";
  else if (f.fields.length > maxFields) e.fields = `Choose at most ${maxFields} research fields.`;

  if (s.attendance && !f.attendance) e.attendance = "Choose how the event takes place.";
  if (s.place) {
    if (!f.city.trim()) e.city = "Enter the city.";
    else if (f.city.trim().length > maxCity) e.city = `The city may be at most ${maxCity} characters.`;
    if (!f.country.trim()) e.country = "Enter the country's two-letter code.";
    else if (!/^[A-Za-z]{2}$/.test(f.country.trim())) e.country = "Use the two-letter ISO code, such as BD.";
  }
  if (s.dates) {
    if (f.startsOn && !dateRe.test(f.startsOn)) e.startsOn = "Enter a valid date.";
    if (f.endsOn && !dateRe.test(f.endsOn)) e.endsOn = "Enter a valid date.";
    else if (f.endsOn && !f.startsOn) e.startsOn = "Enter the first day too.";
    else if (f.startsOn && f.endsOn && f.endsOn < f.startsOn) e.endsOn = "The last day is before the first day.";
  }
  if (s.deadlines) {
    if (!f.timeZone.trim()) e.timeZone = "Choose the time zone the deadlines are stated in.";
    else if (!validZone(f.timeZone.trim())) e.timeZone = "Use AoE, UTC or a zone such as Asia/Dhaka.";
    const sub = withSeconds(f.submissionDeadline);
    if (!f.submissionDeadline) e.submissionDeadline = "Enter the submission deadline.";
    else if (!localRe.test(f.submissionDeadline)) e.submissionDeadline = "Enter a valid date and time.";
    if (f.abstractDeadline && !localRe.test(f.abstractDeadline)) e.abstractDeadline = "Enter a valid date and time.";
    else if (sub && f.abstractDeadline && withSeconds(f.abstractDeadline)! > sub) {
      e.abstractDeadline = "The abstract deadline is after the submission deadline.";
    }
    if (f.notificationAt && !localRe.test(f.notificationAt)) e.notificationAt = "Enter a valid date and time.";
    else if (sub && f.notificationAt && withSeconds(f.notificationAt)! < sub) {
      e.notificationAt = "The notification date is before the submission deadline.";
    }
  }
  const link = urlError(f.officialUrl);
  if (link) e.officialUrl = link;

  const scope = f.scope.trim();
  if (!scope) e.scope = "Describe the venue's scope.";
  else if (scope.length > maxScope) e.scope = `The scope may be at most ${maxScope.toLocaleString("en-US")} characters.`;
  else if (badControl.test(scope)) e.scope = "The scope contains characters that are not allowed.";
  return e;
}

// formFields are the form's inputs in the order they show, so the first error gets the focus.
export const formFieldOrder: FormField[] = [
  "name",
  "shortName",
  "type",
  "rolling",
  "fields",
  "attendance",
  "city",
  "country",
  "startsOn",
  "endsOn",
  "timeZone",
  "submissionDeadline",
  "abstractDeadline",
  "notificationAt",
  "officialUrl",
  "scope",
];
