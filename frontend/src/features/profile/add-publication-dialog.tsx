"use client";

import { type FormEvent, type KeyboardEvent, useId, useRef, useState } from "react";

import { Dialog } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/field";

import { DoiTab, Unavailable } from "./doi-tab";
import { OrcidTab } from "./orcid-tab";
import {
  emptyManual,
  type ManualErrors,
  type ManualValues,
  manualBody,
  publicationError,
  validateManual,
} from "./publication-errors";
import { ManualFields, splitErrors } from "./publication-form";
import { addManualPublication } from "./publications-api";
import { SaveButton } from "./save-button";

type Tab = "doi" | "orcid" | "manual";

const tabs: { id: Tab; label: string }[] = [
  { id: "doi", label: "Add by DOI" },
  { id: "orcid", label: "Import from ORCID" },
  { id: "manual", label: "Add manually" },
];

// addedMessage is the toast after a publication is added; linked means it was already on
// ResearchSphere and the user became one of its authors (FR-5.4).
export function addedMessage(linked: boolean): string {
  return linked ? "Added you as an author" : "Publication added";
}

type AddPublicationDialogProps = {
  orcidId: string | null;
  onClose: () => void;
  // onAdded closes the dialog and refreshes the profile, with the toast message.
  onAdded: (message: string) => void;
  // onChanged refreshes the profile and keeps the dialog open, after an import or once
  // the ORCID iD is saved.
  onChanged: () => void;
  returnFocusId?: string;
};

// AddPublicationDialog is the design's Publications editor (design/screens/ProfileEdit.html)
// as a dialog, with three tabs: Add by DOI (FR-5.2), Import from ORCID (FR-5.3) and Add
// manually (FR-5.1). Every tab stays mounted while the dialog is open, so switching tabs
// keeps what was typed. When the publication database cannot be reached, the DOI tab sends
// the user to Add manually with the DOI filled in and the API's message shown (FR-5.6).
export function AddPublicationDialog({
  orcidId,
  onClose,
  onAdded,
  onChanged,
  returnFocusId,
}: AddPublicationDialogProps) {
  const [tab, setTab] = useState<Tab>("doi");
  const [manual, setManual] = useState<ManualValues>(emptyManual);
  const [manualErrors, setManualErrors] = useState<ManualErrors>({});
  const [manualFormError, setManualFormError] = useState<string>();
  const [fallback, setFallback] = useState<string>();
  const [saving, setSaving] = useState(false);
  const baseId = useId();
  const tabRefs = useRef<Partial<Record<Tab, HTMLButtonElement | null>>>({});
  const dirty = (Object.keys(emptyManual) as (keyof ManualValues)[]).some(
    (k) => manual[k] !== emptyManual[k],
  );

  function onTabKey(event: KeyboardEvent) {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
    if (step === undefined) return;
    event.preventDefault();
    const i = tabs.findIndex((t) => t.id === tab);
    const next = tabs[(i + step + tabs.length) % tabs.length].id;
    setTab(next);
    tabRefs.current[next]?.focus();
  }

  function toManual(doi: string, message: string) {
    setManual({ ...manual, doi });
    setFallback(message);
    setTab("manual");
    tabRefs.current.manual?.focus();
  }

  async function onSubmitManual(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setManualFormError(undefined);
    const found = validateManual(manual);
    setManualErrors(found);
    if (Object.keys(found).length > 0) return;
    setSaving(true);
    const res = await addManualPublication(manualBody(manual));
    setSaving(false);
    if (res.ok) return onAdded(addedMessage(res.data.linked));
    const { field, message } = publicationError(res);
    const split = splitErrors(field, message);
    setManualErrors(split.errors);
    setManualFormError(split.formError);
  }

  const tabId = (t: Tab) => `${baseId}-${t}-tab`;
  const panelId = (t: Tab) => `${baseId}-${t}-panel`;
  return (
    <Dialog title="Add publication" dirty={dirty} onClose={onClose} returnFocusId={returnFocusId}>
      <div className="space-y-4">
        <div role="tablist" aria-label="How to add" onKeyDown={onTabKey} className="flex flex-wrap gap-1 border-b border-rs-border">
          {tabs.map((t) => (
            <button
              key={t.id}
              ref={(el) => {
                tabRefs.current[t.id] = el;
              }}
              id={tabId(t.id)}
              type="button"
              role="tab"
              aria-selected={tab === t.id}
              aria-controls={panelId(t.id)}
              tabIndex={tab === t.id ? 0 : -1}
              onClick={() => setTab(t.id)}
              className={`-mb-px min-h-11 border-b-2 px-3.5 text-rs-body font-medium focus-visible:shadow-focus focus-visible:outline-none ${
                tab === t.id ? "border-rs-accent-strong text-rs-heading" : "border-transparent text-rs-text hover:bg-rs-subtle"
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
        <div role="tabpanel" id={panelId("doi")} aria-labelledby={tabId("doi")} hidden={tab !== "doi"}>
          <DoiTab
            onAdded={(linked) => onAdded(addedMessage(linked))}
            onManual={toManual}
          />
        </div>
        <div role="tabpanel" id={panelId("orcid")} aria-labelledby={tabId("orcid")} hidden={tab !== "orcid"}>
          <OrcidTab orcidId={orcidId} onOrcidSaved={onChanged} onImported={onChanged} onDone={onClose} />
        </div>
        <div role="tabpanel" id={panelId("manual")} aria-labelledby={tabId("manual")} hidden={tab !== "manual"}>
          <form noValidate onSubmit={onSubmitManual} className="space-y-3">
            {fallback && <Unavailable message={fallback} />}
            {manualFormError && <FormError look="rs">{manualFormError}</FormError>}
            <ManualFields idPrefix="add-publication" values={manual} errors={manualErrors} onChange={setManual} />
            <div className="flex justify-end pt-1">
              <SaveButton saving={saving} label="Add publication" />
            </div>
          </form>
        </div>
      </div>
    </Dialog>
  );
}
