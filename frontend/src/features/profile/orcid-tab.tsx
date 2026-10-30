"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { CheckboxField, Field, FormError, rsLegendClass } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";

import { Unavailable } from "./doi-tab";
import { publicationError } from "./publication-errors";
import {
  importOrcidWorks,
  listMyOrcidWorks,
  type OrcidImportResult,
  type OrcidWork,
  type PubFailure,
  setMyOrcidId,
} from "./publications-api";

// importMax is the most put-codes one import takes.
const importMax = 100;

function workLabel(w: OrcidWork): string {
  return [w.title, w.year].filter((v) => v !== null).join(" · ");
}

// summary is the import result in words, such as "2 added, 1 linked to an existing record,
// 0 skipped".
export function importSummary(r: OrcidImportResult): string {
  return `${r.created} added, ${r.linked} linked to ${r.linked === 1 ? "an existing record" : "existing records"}, ${r.skipped} skipped (already on your profile).`;
}

// OrcidTab is "Import from ORCID" (FR-5.3). Without an ORCID iD on the profile it asks for
// one and saves it first. "List my works" shows the iD's public works as a checklist; works
// already on the profile are checked and cannot be changed. "Import N selected" imports
// only the ticked ones and then says what happened to them.
export function OrcidTab({
  orcidId,
  onOrcidSaved,
  onImported,
  onDone,
}: {
  orcidId: string | null;
  onOrcidSaved: () => void;
  onImported: () => void;
  onDone: () => void;
}) {
  const [savedId, setSavedId] = useState(orcidId);
  const [input, setInput] = useState("");
  const [works, setWorks] = useState<OrcidWork[] | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [result, setResult] = useState<OrcidImportResult | null>(null);
  const [orcidError, setOrcidError] = useState<string>();
  const [formError, setFormError] = useState<string>();
  const [unavailable, setUnavailable] = useState<string>();
  const [busy, setBusy] = useState<"list" | "import" | null>(null);

  function clear() {
    setOrcidError(undefined);
    setFormError(undefined);
    setUnavailable(undefined);
  }

  function fail(res: PubFailure) {
    if (res.code === "metadata_unavailable") return setUnavailable(res.message);
    const { field, message } = publicationError(res);
    if (field === "orcid") setOrcidError(message);
    else setFormError(message);
  }

  async function list(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    clear();
    setResult(null);
    if (savedId === null) {
      if (input.trim() === "") return setOrcidError("Enter your ORCID iD.");
      setBusy("list");
      const saved = await setMyOrcidId(input.trim());
      if (!saved.ok) {
        setBusy(null);
        return fail(saved);
      }
      setSavedId(saved.data.orcidId ?? input.trim());
      onOrcidSaved();
    }
    setBusy("list");
    const res = await listMyOrcidWorks();
    setBusy(null);
    if (!res.ok) return fail(res);
    setWorks(res.data.items);
    setSelected(new Set());
  }

  async function runImport() {
    clear();
    setBusy("import");
    const res = await importOrcidWorks([...selected]);
    setBusy(null);
    if (!res.ok) return fail(res);
    setResult(res.data);
    setWorks(null);
    setSelected(new Set());
    onImported();
  }

  function toggle(putCode: string, on: boolean) {
    const next = new Set(selected);
    if (on) next.add(putCode);
    else next.delete(putCode);
    setSelected(next);
  }

  const n = selected.size;
  return (
    <div className="space-y-3">
      <form noValidate onSubmit={list} className="flex flex-wrap items-start gap-2">
        {savedId === null ? (
          <Field
            id="publication-orcid"
            label="ORCID iD"
            look="rs"
            className="min-w-0 flex-[1_1_240px]"
            placeholder="0000-0002-1825-0097"
            error={orcidError}
            value={input}
            onChange={(e) => setInput(e.target.value)}
          />
        ) : (
          <div className="flex min-w-0 flex-[1_1_240px] flex-col gap-1">
            <p className="text-rs-body font-medium text-rs-text">ORCID iD</p>
            <p className="flex h-11 items-center text-rs-body text-rs-text">{savedId}</p>
            {orcidError && (
              <p role="alert" className="text-rs-caption text-rs-danger">
                {orcidError}
              </p>
            )}
          </div>
        )}
        <button
          type="submit"
          disabled={busy !== null}
          className={`mt-6 ${rsButtonClass({ variant: "outline", size: "md" })}`}
        >
          {busy === "list" && <Spinner />}
          {busy === "list" ? "Listing…" : savedId === null ? "Save & list my works" : "List my works"}
        </button>
      </form>
      {formError && <FormError look="rs">{formError}</FormError>}
      {unavailable && <Unavailable message={unavailable} />}
      {result && (
        <div role="status" className="space-y-3 rounded-rs-md bg-rs-accent-soft px-3 py-2.5 text-rs-body text-rs-heading">
          <p>
            <span className="font-medium">Import finished.</span> {importSummary(result)}
          </p>
          <button type="button" onClick={onDone} className={rsButtonClass({ size: "sm" })}>
            Done
          </button>
        </div>
      )}
      {works && works.length === 0 && (
        <p className="text-rs-body text-rs-text-muted">This ORCID record has no public works.</p>
      )}
      {works && works.length > 0 && (
        <fieldset className="flex flex-col">
          <legend className={rsLegendClass}>Your ORCID works</legend>
          {works.map((w) => (
            <CheckboxField
              key={w.putCode}
              label={w.linkedToMe ? `${workLabel(w)} · already on your profile` : workLabel(w)}
              checked={w.linkedToMe || selected.has(w.putCode)}
              disabled={w.linkedToMe || busy !== null}
              onChange={(e) => toggle(w.putCode, e.target.checked)}
            />
          ))}
          {n > importMax && (
            <p className="mt-1 text-rs-caption text-rs-danger">
              You can import up to {importMax} works at a time.
            </p>
          )}
          <button
            type="button"
            onClick={runImport}
            disabled={n === 0 || n > importMax || busy !== null}
            className={`mt-2 self-start ${rsButtonClass({ size: "md" })}`}
          >
            {busy === "import" && <Spinner />}
            {busy === "import" ? "Importing…" : `Import ${n} selected`}
          </button>
        </fieldset>
      )}
    </div>
  );
}
