"use client";

import { rsButtonClass } from "@/components/ui/button";
import { CheckboxField, rsLegendClass, SelectField } from "@/components/ui/field";
import { homeCard } from "@/features/home/cards";

import type { PeopleSearch, PeopleSearchPage } from "./api";

type Facet = { name: string; count?: number };

type SearchFiltersProps = {
  search: PeopleSearch;
  // facets are those of the latest results; null before any have loaded.
  facets: PeopleSearchPage["facets"] | null;
  onChange: (filters: Pick<PeopleSearch, "institution" | "interests">) => void;
};

const sameName = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

// withSelected lists every facet and, after them, the selected names the facets lack (the
// API lists selected interests itself, but not before results have loaded), so a selected
// filter can always be cleared.
function withSelected(facets: Facet[], selected: string[]): Facet[] {
  const missing = selected.filter((name) => !facets.some((f) => sameName(f.name, name)));
  return [...facets, ...missing.map((name) => ({ name }))];
}

const label = ({ name, count }: Facet) => (count === undefined ? name : `${name} (${count})`);

// SearchFilters is the Filters aside of the search page (FR-6.3): the institution select,
// the research interest checkboxes, each with its result count, and Reset filters.
export function SearchFilters({ search, facets, onChange }: SearchFiltersProps) {
  const { institution, interests } = search;
  const institutions = withSelected(facets?.institutions ?? [], institution ? [institution] : []);
  const interestFacets = withSelected(facets?.interests ?? [], interests);
  const filtered = institution !== "" || interests.length > 0;
  const selectedInstitution = institutions.find((f) => sameName(f.name, institution))?.name ?? "";

  function toggle(name: string, checked: boolean) {
    onChange({
      institution,
      interests: checked ? [...interests, name] : interests.filter((n) => !sameName(n, name)),
    });
  }

  return (
    <aside
      aria-label="Filters"
      className={`${homeCard} flex min-w-0 flex-[1_1_220px] flex-col gap-3 p-4 min-[641px]:max-w-[260px]`}
    >
      <h2 className="text-rs-lead leading-rs-snug font-medium text-rs-text">Filters</h2>
      <SelectField
        id="search-institution"
        label="Institution"
        look="rs"
        value={selectedInstitution}
        onChange={(event) => onChange({ institution: event.target.value, interests })}
      >
        <option value="">Any institution</option>
        {institutions.map((f) => (
          <option key={f.name} value={f.name}>
            {label(f)}
          </option>
        ))}
      </SelectField>
      {interestFacets.length > 0 && (
        <fieldset className="flex flex-col">
          <legend className={rsLegendClass}>Research interest</legend>
          {interestFacets.map((f) => (
            <CheckboxField
              key={f.name}
              label={label(f)}
              checked={interests.some((n) => sameName(n, f.name))}
              onChange={(event) => toggle(f.name, event.target.checked)}
            />
          ))}
        </fieldset>
      )}
      <button
        type="button"
        disabled={!filtered}
        onClick={() => onChange({ institution: "", interests: [] })}
        className={`${rsButtonClass({ variant: "secondary", size: "sm", full: true })} h-10`}
      >
        Reset filters
      </button>
    </aside>
  );
}
