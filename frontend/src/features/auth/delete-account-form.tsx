"use client";

import Link from "next/link";
import { type FormEvent, useState } from "react";

import { SectionCard } from "@/components/section-layout";
import { rsButtonClass } from "@/components/ui/button";
import { CheckboxField, Field, FormError } from "@/components/ui/field";

import { markAccountDeleted } from "./account-deleted";
import { deleteAccount, type ProjectRef } from "./api";

// leave sends the browser to the landing page with a full page load. A client-side
// navigation would leave this tab's session store and cached data signed in, and
// marking the session signed out first would make RequireSession send the page to
// /login instead.
function leave(): void {
  window.location.replace("/");
}

// DeleteAccountForm deletes the signed-in user's account after the password and an
// explicit confirmation (FR-2.8). While the user owns projects that have other members,
// the server refuses and the form lists them (FR-2.9).
export function DeleteAccountForm({ onDeleted = leave }: { onDeleted?: () => void }) {
  const [password, setPassword] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [passwordError, setPasswordError] = useState<string>();
  const [formError, setFormError] = useState<string>();
  const [blocking, setBlocking] = useState<ProjectRef[]>([]);
  const [submitting, setSubmitting] = useState(false);

  const ready = password !== "" && confirmed && !submitting;

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!ready) return;
    setPasswordError(undefined);
    setFormError(undefined);
    setBlocking([]);

    setSubmitting(true);
    const res = await deleteAccount(password);
    if (res.ok) {
      markAccountDeleted();
      onDeleted();
      return;
    }
    setSubmitting(false);
    if (res.code === "wrong_password") setPasswordError(res.message);
    else setFormError(res.message);
    if (res.code === "owns_shared_project") setBlocking(res.projects ?? []);
  }

  return (
    <SectionCard id="delete-account" title="Delete account" tone="danger">
      <p className="text-rs-body text-rs-text">
        Deletes your profile, posts, comments, likes and connections right away, with the projects where you are the
        only member. Publications you share with a co-author stay on their profile. It cannot be undone.
      </p>
      <form noValidate onSubmit={onSubmit} className="flex flex-col gap-3">
        {blocking.length > 0 ? (
          <div
            role="alert"
            className="rounded-rs-md border border-rs-warning-line bg-rs-warning-soft p-3 text-rs-body text-rs-warning"
          >
            <p>{formError}</p>
            <ul className="mt-1 list-disc pl-5">
              {blocking.map((p) => (
                <li key={p.id}>
                  You own{" "}
                  <Link href={`/projects/${p.id}`} className="font-medium text-rs-warning underline">
                    {p.title}
                  </Link>
                </li>
              ))}
            </ul>
            <p className="mt-1">Transfer ownership or delete each project before deleting your account.</p>
          </div>
        ) : formError ? (
          <FormError look="rs">{formError}</FormError>
        ) : null}
        <Field
          look="rs"
          className="max-w-[360px]"
          id="deletePassword"
          name="password"
          label="Enter your password to confirm"
          type="password"
          autoComplete="current-password"
          error={passwordError}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <CheckboxField
          label="I understand this cannot be undone"
          checked={confirmed}
          onChange={(e) => setConfirmed(e.target.checked)}
        />
        <button
          type="submit"
          disabled={!ready}
          className={`self-start ${rsButtonClass({ variant: "destructive", size: "form" })}`}
        >
          {submitting ? "Deleting…" : "Delete my account"}
        </button>
      </form>
    </SectionCard>
  );
}
