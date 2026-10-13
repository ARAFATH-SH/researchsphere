"use client";

import { type FormEvent, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Field, FormError } from "@/components/ui/field";

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
    <section aria-labelledby="delete-account-title">
      <Card className="grid gap-6 border-danger/40 p-6 md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
        <div>
          <h2 id="delete-account-title" className="text-lg font-semibold text-danger">
            Delete account
          </h2>
          <p className="mt-1 text-sm text-text-muted">
            This deletes your profile, posts, comments, likes, connections and the projects where you are the only
            member. Publications you share with a co-author stay for that co-author. It cannot be undone.
          </p>
        </div>
        <form noValidate onSubmit={onSubmit} className="space-y-4">
          {formError ? <FormError>{formError}</FormError> : null}
          {blocking.length > 0 ? (
            <div className="text-sm">
              <p className="font-semibold">Transfer or delete these projects first:</p>
              <ul className="mt-1 list-disc pl-5">
                {blocking.map((p) => (
                  <li key={p.id}>{p.title}</li>
                ))}
              </ul>
            </div>
          ) : null}
          <Field
            id="deletePassword"
            name="password"
            label="Password"
            type="password"
            autoComplete="current-password"
            error={passwordError}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-0.5 h-4 w-4 accent-danger"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            I understand this cannot be undone
          </label>
          <button type="submit" disabled={!ready} className={buttonClass({ variant: "danger" })}>
            {submitting ? "Deleting…" : "Delete account"}
          </button>
        </form>
      </Card>
    </section>
  );
}
