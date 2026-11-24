"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { SectionCard } from "@/components/section-layout";
import { rsButtonClass } from "@/components/ui/button";
import { chipClass } from "@/components/ui/chip";
import { CheckboxField, FormError, rsInputClass, rsSelectClass } from "@/components/ui/field";
import { PageNav } from "@/components/ui/page-nav";
import type { Result } from "@/lib/api/call";

import {
  getVenue,
  listResearchFields,
  listVenues,
  type ResearchField,
  type Venue,
  type VenuePage,
  type VenueSummary,
  type VenueType,
} from "../api";
import { isVenueType, sourceLabels, typeLabels, venueTypes } from "../labels";
import { minQueryLength, venuesPageSize } from "../list-params";
import { venueTimeLabel } from "../time";
import { TypeBadge } from "../venue-bits";
import { importSummary, ImportVenuesDialog } from "./import-venues-dialog";
import { RemoveVenueDialog } from "./remove-venue-dialog";
import { VenueFormDialog } from "./venue-form-dialog";

type Filters = { q: string; type: VenueType | ""; field: string; past: boolean; journals: boolean; page: number };

type Open =
  | { kind: "none" }
  | { kind: "add" }
  | { kind: "edit"; venue: Venue; from: string }
  | { kind: "remove"; venue: VenueSummary; from: string }
  | { kind: "import" };

const cellClass = "px-2 first:pl-4 last:pr-4";

// AdminVenues is the administrator's venue list (FR-15.1), as in the Venues card of
// design/screens/Admin.html: search and filters, add, edit, remove with a reason, and the file
// import with a dry run first. A venue of the automatic import that an administrator edited
// shows a Locked badge (only administrators are told).
export function AdminVenues() {
  const [filters, setFilters] = useState<Filters>({ q: "", type: "", field: "", past: true, journals: false, page: 1 });
  const [draft, setDraft] = useState("");
  const [fields, setFields] = useState<ResearchField[]>([]);
  const [reload, setReload] = useState(0);
  const key = `${JSON.stringify(filters)}#${reload}`;
  const [result, setResult] = useState<{ key: string; res: Result<VenuePage> } | null>(null);
  const [open, setOpen] = useState<Open>({ kind: "none" });
  const [notice, setNotice] = useState("");
  const [loadError, setLoadError] = useState("");

  useEffect(() => {
    let current = true;
    void listResearchFields().then((res) => {
      if (current && res.ok) setFields(res.data);
    });
    return () => {
      current = false;
    };
  }, []);

  useEffect(() => {
    const f = filters;
    let current = true;
    const q = f.q.trim();
    void listVenues({
      page: f.page,
      pageSize: venuesPageSize,
      rolling: f.journals,
      includePast: !f.journals && f.past,
      types: f.type ? [f.type] : [],
      fields: f.field ? [f.field] : [],
      q: q.length >= minQueryLength ? q : undefined,
    }).then((res) => {
      if (current) setResult({ key, res });
    });
    return () => {
      current = false;
    };
  }, [filters, key]);

  // Typing waits a moment before it searches.
  useEffect(() => {
    if (draft === filters.q) return;
    const t = setTimeout(() => setFilters((f) => ({ ...f, q: draft, page: 1 })), 300);
    return () => clearTimeout(t);
  }, [draft, filters.q]);

  const change = (c: Partial<Filters>) => setFilters((f) => ({ ...f, ...c, page: 1 }));
  const refresh = () => setReload((n) => n + 1);
  const close = () => setOpen({ kind: "none" });

  async function startEdit(v: VenueSummary) {
    setLoadError("");
    const res = await getVenue(v.id);
    if (res.ok) setOpen({ kind: "edit", venue: res.data, from: `venue-edit-${v.id}` });
    else setLoadError(res.code === "not_found" ? "This venue has been removed." : res.message);
  }

  const res = result?.key === key ? result.res : null;
  const actions = (
    <div className="flex flex-wrap items-center gap-2">
      <button id="venue-import-open" type="button" onClick={() => setOpen({ kind: "import" })} className={rsButtonClass({ variant: "outline", size: "xs" })}>
        Import from file
      </button>
      <button id="venue-add-open" type="button" onClick={() => setOpen({ kind: "add" })} className={rsButtonClass({ size: "xs" })}>
        + Add venue
      </button>
    </div>
  );

  return (
    <SectionCard id="venues" title="Venues" header aside={actions}>
      {notice && (
        <p role="status" className="mx-4 mt-3 rounded-rs-md bg-rs-accent-soft px-3 py-2.5 text-rs-body text-rs-heading">
          {notice}
        </p>
      )}
      <div className="flex flex-wrap items-end gap-3 px-4 pt-3">
        <label className="flex min-w-0 flex-[1_1_200px] flex-col gap-1 text-rs-body font-medium">
          Search venues
          <input type="search" className={rsInputClass} value={draft} maxLength={100} onChange={(e) => setDraft(e.target.value)} />
        </label>
        <label className="flex min-w-0 flex-[1_1_140px] flex-col gap-1 text-rs-body font-medium">
          Type
          <select
            className={rsSelectClass}
            value={filters.type}
            onChange={(e) => change({ type: isVenueType(e.target.value) ? e.target.value : "" })}
          >
            <option value="">All types</option>
            {venueTypes.map((t) => (
              <option key={t} value={t}>
                {typeLabels[t]}
              </option>
            ))}
          </select>
        </label>
        <label className="flex min-w-0 flex-[1_1_180px] flex-col gap-1 text-rs-body font-medium">
          Field
          <select className={rsSelectClass} value={filters.field} onChange={(e) => change({ field: e.target.value })}>
            <option value="">All fields</option>
            {fields.map((f) => (
              <option key={f.key} value={f.key}>
                {f.label}
              </option>
            ))}
          </select>
        </label>
        <CheckboxField
          label="Past deadlines too"
          checked={filters.past}
          disabled={filters.journals}
          onChange={(e) => change({ past: e.target.checked })}
          className="min-h-11"
        />
        <CheckboxField
          label="Rolling journals"
          checked={filters.journals}
          onChange={(e) => change({ journals: e.target.checked })}
          className="min-h-11"
        />
      </div>
      {loadError && (
        <div className="px-4 pt-3">
          <FormError look="rs">{loadError}</FormError>
        </div>
      )}
      {!res && (
        <p role="status" className="px-4 py-3 text-rs-body text-rs-text-muted">
          Loading venues…
        </p>
      )}
      {res && !res.ok && (
        <div className="px-4 py-3">
          <FormError look="rs">{res.message}</FormError>
        </div>
      )}
      {res?.ok && (
        <>
          <div className="overflow-x-auto py-2">
            <table className="w-full min-w-[720px] border-collapse text-left text-rs-body text-rs-text">
              <caption className="sr-only">Venues, {res.data.total} in all</caption>
              <thead className="text-rs-caption text-rs-text-muted">
                <tr>
                  {["Name", "Type", "Field", "Deadline", "Source", "Actions"].map((h) => (
                    <th key={h} scope="col" className={`${cellClass} py-2 font-medium ${h === "Actions" ? "text-right" : ""}`}>
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {res.data.items.length === 0 ? (
                  <tr className="border-t border-rs-divider">
                    <td colSpan={6} className={`${cellClass} py-2.5 text-rs-text-muted`}>
                      No venues match.
                    </td>
                  </tr>
                ) : (
                  res.data.items.map((v) => (
                    <tr key={v.id} className="border-t border-rs-divider align-top hover:bg-rs-subtle">
                      <td className={`${cellClass} py-2.5 font-medium`}>
                        <Link href={`/events/${v.id}`} className="text-rs-text hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none">
                          {v.name}
                        </Link>
                        {v.shortName && <span className="font-normal text-rs-text-muted"> · {v.shortName}</span>}
                      </td>
                      <td className={`${cellClass} py-2.5`}>
                        <TypeBadge type={v.type} />
                      </td>
                      <td className={`${cellClass} py-2.5`}>{v.fields.map((f) => f.label).join(", ")}</td>
                      <td className={`${cellClass} py-2.5 whitespace-nowrap ${v.past ? "text-rs-text-muted" : ""}`}>
                        {v.submissionDeadline ? venueTimeLabel(v.submissionDeadline, v.timeZone ?? "UTC") : "Rolling"}
                        {v.past && <span className="block text-rs-caption">Passed</span>}
                      </td>
                      <td className={`${cellClass} py-2.5 text-rs-text-muted`}>
                        {sourceLabels[v.source]}
                        {v.locked && <span className={`${chipClass} ml-1.5 bg-rs-warning-soft text-rs-warning`}>Locked</span>}
                      </td>
                      <td className={`${cellClass} py-2.5 text-right whitespace-nowrap`}>
                        <button
                          id={`venue-edit-${v.id}`}
                          type="button"
                          aria-label={`Edit ${v.name}`}
                          onClick={() => void startEdit(v)}
                          className={rsButtonClass({ variant: "secondary", size: "xs" })}
                        >
                          Edit
                        </button>{" "}
                        <button
                          id={`venue-remove-${v.id}`}
                          type="button"
                          aria-label={`Remove ${v.name}`}
                          onClick={() => setOpen({ kind: "remove", venue: v, from: `venue-remove-${v.id}` })}
                          className="h-[34px] rounded-rs-pill px-2.5 text-rs-caption font-medium text-rs-danger hover:bg-rs-danger-soft focus-visible:shadow-focus focus-visible:outline-none"
                        >
                          Remove
                        </button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
          {res.data.total > res.data.pageSize && (
            <PageNav
              page={res.data.page}
              pageSize={res.data.pageSize}
              total={res.data.total}
              onPage={(page) => setFilters((f) => ({ ...f, page }))}
            />
          )}
        </>
      )}

      {open.kind === "add" && (
        <VenueFormDialog
          fields={fields}
          returnFocusId="venue-add-open"
          onClose={close}
          onSaved={(v) => {
            close();
            setNotice(`Added ${v.name}.`);
            refresh();
          }}
        />
      )}
      {open.kind === "edit" && (
        <VenueFormDialog
          venue={open.venue}
          fields={fields}
          returnFocusId={open.from}
          onClose={close}
          onSaved={(v) => {
            close();
            setNotice(`Saved ${v.name}.`);
            refresh();
          }}
        />
      )}
      {open.kind === "remove" && (
        <RemoveVenueDialog
          venue={open.venue}
          returnFocusId={open.from}
          onClose={close}
          onRemoved={() => {
            setNotice(`Removed ${open.venue.name}.`);
            close();
            refresh();
          }}
        />
      )}
      {open.kind === "import" && (
        <ImportVenuesDialog
          returnFocusId="venue-import-open"
          onClose={close}
          onImported={(r) => {
            close();
            setNotice(`Import finished: ${importSummary(r)}`);
            refresh();
          }}
        />
      )}
    </SectionCard>
  );
}
