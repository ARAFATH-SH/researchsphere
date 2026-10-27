"use client";

import {
  type FormEvent,
  type KeyboardEvent,
  useEffect,
  useMemo,
  useState,
} from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import {
  Field,
  FieldRow,
  FormError,
  SelectField,
} from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";

import {
  addLink,
  type Contact,
  getMyContact,
  type LinkKind,
  type ProfileLink,
  removeLink,
  updateMyContact,
  type Visibility,
} from "./api";
import {
  type ContactErrors,
  type ContactValues,
  daysIn,
  fieldForContactCode,
  linkKinds,
  linkLabels,
  MAX_LINKS,
  monthName,
  normalizePhone,
  validateContact,
  validateLinkURL,
  visibilityLabels,
} from "./contact-validation";
import { LinkIcon } from "./link-icon";
import { FormActions } from "./save-button";
import { genericError } from "./validation";

type ContactDialogProps = {
  onClose: () => void;
  // onSaved closes the dialog and reloads the profile.
  onSaved: () => void;
  // onLinksChanged reloads the profile behind the dialog after a link is added or removed.
  onLinksChanged: () => void;
  returnFocusId?: string;
};

// ContactDialog edits the owner's contact info and who may see each item, and their
// links (FR-4.7). The email address is the account's and is shown, not edited.
export function ContactDialog({
  onClose,
  onSaved,
  onLinksChanged,
  returnFocusId,
}: ContactDialogProps) {
  const [loaded, setLoaded] = useState<
    { contact: Contact } | { failed: true } | null
  >(null);
  const [formDirty, setFormDirty] = useState(false);
  const [linkDraft, setLinkDraft] = useState(false);

  useEffect(() => {
    let current = true;
    void getMyContact().then((res) => {
      if (current) setLoaded(res.ok ? { contact: res.data } : { failed: true });
    });
    return () => {
      current = false;
    };
  }, []);

  return (
    <Dialog
      title="Contact info"
      dirty={formDirty || linkDraft}
      onClose={onClose}
      returnFocusId={returnFocusId}
    >
      {loaded === null ? (
        <p role="status" className="text-rs-text-muted">
          Loading contact info…
        </p>
      ) : "failed" in loaded ? (
        <FormError look="rs">
          We couldn&apos;t load your contact info. Close this dialog and try
          again.
        </FormError>
      ) : (
        <div className="space-y-8">
          <ContactForm
            contact={loaded.contact}
            onDirtyChange={setFormDirty}
            onSaved={onSaved}
          />
          <LinkEditor
            initial={loaded.contact.links}
            onChange={onLinksChanged}
            onDraftChange={setLinkDraft}
          />
        </div>
      )}
    </Dialog>
  );
}

function valuesOf(c: Contact): ContactValues {
  return {
    emailVisibility: c.emailVisibility,
    phone: c.phone ?? "",
    phoneVisibility: c.phoneVisibility,
    month: c.birthday ? String(c.birthday.month) : "",
    day: c.birthday ? String(c.birthday.day) : "",
    birthdayVisibility: c.birthdayVisibility,
  };
}

const visibilities = Object.keys(visibilityLabels) as Visibility[];

function VisibilitySelect({
  id,
  label,
  value,
  onChange,
}: {
  id: string;
  label: string;
  value: Visibility;
  onChange: (v: Visibility) => void;
}) {
  return (
    <SelectField
      id={id}
      label={label}
      look="rs"
      value={value}
      onChange={(e) => onChange(e.target.value as Visibility)}
    >
      {visibilities.map((v) => (
        <option key={v} value={v}>
          {visibilityLabels[v]}
        </option>
      ))}
    </SelectField>
  );
}

type ContactFormProps = {
  contact: Contact;
  onDirtyChange: (dirty: boolean) => void;
  onSaved: () => void;
};

function ContactForm({ contact, onDirtyChange, onSaved }: ContactFormProps) {
  const initial = useMemo(() => valuesOf(contact), [contact]);
  const [values, setValues] = useState(initial);
  const [errors, setErrors] = useState<ContactErrors>({});
  const [formError, setFormError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const days = values.month === "" ? 31 : daysIn(Number(values.month));

  function update(next: ContactValues) {
    setValues(next);
    onDirtyChange(
      (Object.keys(initial) as (keyof ContactValues)[]).some(
        (k) => next[k] !== initial[k],
      ),
    );
  }

  function setMonth(month: string) {
    // A day the new month does not have, such as 30 for February, is cleared.
    const day =
      values.day !== "" &&
      month !== "" &&
      Number(values.day) > daysIn(Number(month))
        ? ""
        : values.day;
    update({ ...values, month, day });
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    const found = validateContact(values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;

    const phone = normalizePhone(values.phone);
    setSaving(true);
    const res = await updateMyContact({
      emailVisibility: values.emailVisibility,
      phone: phone === "" ? null : phone,
      phoneVisibility: values.phoneVisibility,
      birthday:
        values.month === ""
          ? null
          : { month: Number(values.month), day: Number(values.day) },
      birthdayVisibility: values.birthdayVisibility,
    });
    setSaving(false);
    if (res.ok) return onSaved();
    const field = fieldForContactCode(res.code);
    if (field) setErrors({ [field]: res.message });
    else setFormError(genericError);
  }

  const birthdayError = errors.birthday;
  return (
    <form noValidate onSubmit={onSubmit} className="space-y-6">
      {formError ? <FormError look="rs">{formError}</FormError> : null}
      <fieldset className="space-y-3">
        <legend className="mb-3 text-rs-lead leading-rs-snug font-medium">Email</legend>
        <p className="break-all text-rs-text">{contact.email}</p>
        <p className="text-rs-caption text-rs-text-muted">
          This is the address you sign in with. Change it in your account
          settings.
        </p>
        <VisibilitySelect
          id="contact-email-visibility"
          label="Who can see your email"
          value={values.emailVisibility}
          onChange={(v) => update({ ...values, emailVisibility: v })}
        />
      </fieldset>

      <fieldset className="space-y-3">
        <legend className="mb-3 text-rs-lead leading-rs-snug font-medium">Phone</legend>
        <Field
          id="contact-phone"
          name="phone"
          label="Phone number"
          look="rs"
          type="tel"
          autoComplete="tel"
          inputMode="tel"
          placeholder="+8801712345678"
          hint="International format, starting with + and the country code."
          error={errors.phone}
          value={values.phone}
          onChange={(e) => update({ ...values, phone: e.target.value })}
        />
        <VisibilitySelect
          id="contact-phone-visibility"
          label="Who can see your phone number"
          value={values.phoneVisibility}
          onChange={(v) => update({ ...values, phoneVisibility: v })}
        />
      </fieldset>

      <fieldset
        className="space-y-3"
        aria-describedby={
          birthdayError ? "contact-birthday-error" : "contact-birthday-hint"
        }
      >
        <legend className="mb-3 text-rs-lead leading-rs-snug font-medium">Birthday</legend>
        <FieldRow>
          <SelectField
            id="contact-birth-month"
            label="Month"
            look="rs"
            value={values.month}
            aria-invalid={birthdayError ? true : undefined}
            onChange={(e) => setMonth(e.target.value)}
          >
            <option value="">Not set</option>
            {Array.from({ length: 12 }, (_, i) => (
              <option key={i + 1} value={String(i + 1)}>
                {monthName(i + 1)}
              </option>
            ))}
          </SelectField>
          <SelectField
            id="contact-birth-day"
            label="Day"
            look="rs"
            value={values.day}
            aria-invalid={birthdayError ? true : undefined}
            onChange={(e) => update({ ...values, day: e.target.value })}
          >
            <option value="">Not set</option>
            {Array.from({ length: days }, (_, i) => (
              <option key={i + 1} value={String(i + 1)}>
                {i + 1}
              </option>
            ))}
          </SelectField>
        </FieldRow>
        {birthdayError ? (
          <p
            id="contact-birthday-error"
            className="text-rs-caption text-rs-danger"
          >
            {birthdayError}
          </p>
        ) : (
          <p
            id="contact-birthday-hint"
            className="text-rs-caption text-rs-text-muted"
          >
            Day and month only; the year is never asked for.
          </p>
        )}
        <VisibilitySelect
          id="contact-birthday-visibility"
          label="Who can see your birthday"
          value={values.birthdayVisibility}
          onChange={(v) => update({ ...values, birthdayVisibility: v })}
        />
      </fieldset>
      <FormActions saving={saving} />
    </form>
  );
}

type LinkEditorProps = {
  initial: ProfileLink[];
  onChange: () => void;
  onDraftChange: (hasDraft: boolean) => void;
};

// LinkEditor adds and removes links at once, like tags; it has no Save button. Links are
// visible to every logged-in user.
function LinkEditor({ initial, onChange, onDraftChange }: LinkEditorProps) {
  const [links, setLinks] = useState(initial);
  const [kind, setKind] = useState<LinkKind>("website");
  const [url, setUrl] = useState("");
  const [urlError, setUrlError] = useState<string>();
  const [error, setError] = useState<string>();
  const [adding, setAdding] = useState(false);
  const full = links.length >= MAX_LINKS;

  async function add() {
    if (adding || full) return;
    setError(undefined);
    const invalid = validateLinkURL(url);
    setUrlError(invalid);
    if (invalid) return;
    setAdding(true);
    const res = await addLink(kind, url.trim());
    setAdding(false);
    if (res.ok) {
      setLinks((ls) => [...ls, res.data]);
      setUrl("");
      onDraftChange(false);
      onChange();
      return;
    }
    if (res.code === "invalid_link_url") setUrlError(res.message);
    else
      setError(
        res.code === "link_limit_reached" || res.code === "invalid_link_kind"
          ? res.message
          : genericError,
      );
  }

  async function remove(link: ProfileLink) {
    setError(undefined);
    const res = await removeLink(link.id);
    // not_found means it is already gone, which is what the user asked for.
    if (res.ok || res.code === "not_found") {
      setLinks((ls) => ls.filter((l) => l.id !== link.id));
      onChange();
    } else setError(genericError);
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter") {
      e.preventDefault();
      void add();
    }
  }

  return (
    <section
      aria-labelledby="contact-links-title"
      className="space-y-3 border-t border-rs-divider pt-6"
    >
      <div>
        <h3
          id="contact-links-title"
          className="text-rs-lead leading-rs-snug font-medium"
        >
          Links
        </h3>
        <p className="text-rs-caption text-rs-text-muted">
          Visible to all logged-in users. {links.length} / {MAX_LINKS}
        </p>
      </div>
      {error ? <FormError look="rs">{error}</FormError> : null}
      {links.length > 0 && (
        <ul aria-label="Your links" className="space-y-2">
          {links.map((l) => (
            <li
              key={l.id}
              className="flex items-center gap-3 rounded-rs-md border border-rs-border px-3 py-2"
            >
              <span className="text-rs-text-muted">
                <LinkIcon kind={l.kind} />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block font-medium text-rs-text">
                  {linkLabels[l.kind]}
                </span>
                <span className="block truncate text-rs-text-muted">
                  {l.url}
                </span>
              </span>
              <button
                type="button"
                aria-label={`Remove ${linkLabels[l.kind]} link ${l.url}`}
                onClick={() => void remove(l)}
                className="rounded-rs-pill p-1.5 text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
              >
                <svg
                  aria-hidden="true"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={1.75}
                  strokeLinecap="round"
                  className="size-5"
                >
                  <path d="M6 6l12 12M18 6 6 18" />
                </svg>
              </button>
            </li>
          ))}
        </ul>
      )}
      {full ? (
        <p className="text-rs-text-muted">
          You have reached the limit of {MAX_LINKS} links. Remove one to add
          another.
        </p>
      ) : (
        <div className="space-y-3">
          <FieldRow>
            <SelectField
              id="contact-link-kind"
              label="Kind"
              look="rs"
              value={kind}
              onChange={(e) => setKind(e.target.value as LinkKind)}
            >
              {linkKinds.map((k) => (
                <option key={k} value={k}>
                  {linkLabels[k]}
                </option>
              ))}
            </SelectField>
            <Field
              id="contact-link-url"
              label="URL"
              look="rs"
              type="url"
              inputMode="url"
              placeholder="https://"
              error={urlError}
              value={url}
              onChange={(e) => {
                setUrl(e.target.value);
                setUrlError(undefined);
                onDraftChange(e.target.value.trim() !== "");
              }}
              onKeyDown={onKeyDown}
            />
          </FieldRow>
          <button
            type="button"
            disabled={adding}
            onClick={() => void add()}
            className={rsButtonClass({
              variant: "outline",
              size: "form",
              full: true,
            })}
          >
            {adding && <Spinner />}
            {adding ? "Adding…" : "Add link"}
          </button>
        </div>
      )}
    </section>
  );
}
