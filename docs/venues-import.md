# Venue import file (FR-14.5, interim)

An administrator adds and updates venues in bulk with `POST /api/v1/admin/venues/import`
(multipart part `file`). This is the interim form of FR-14.5: no public call-for-papers listing
has been chosen yet (SRS 8.2, `docs/decisions.md` 2026-11-24), so the file is prepared by hand or
by a script and uploaded. `sourceKey` is ready for an automatic fetcher later.

Send `?dryRun=true` first: it reports exactly what the import would do and changes nothing.

## Limits

- At most 1 MiB and 1,000 venues. Over the size: 413 `file_too_large`. Over the rows: 400
  `too_many_rows`.
- UTF-8 text. A byte-order mark at the start is ignored.

## Format

A name ending in `.csv` is read as CSV and `.json` as JSON. With any other name, a file whose
first non-blank character is `[` is JSON and anything else is CSV.

**CSV:** the first line is the header. Column names match the keys below, ignoring letter case
and surrounding spaces; their order is free and a column may be left out (its cells count as
empty). An unknown or repeated column refuses the whole file (400 `invalid_import_file`), so a
typo in a header is never silently ignored. In `fields`, separate keys with `;`. Quote a cell
that holds a comma or a line break, as CSV does (`"Line one` / `line two"`).

**JSON:** an array of objects with the keys below. `fields` is an array of strings; every other
value is a string (or `null`, which counts as empty). An unknown key refuses the whole file.

## Columns

The rules are those of `VenueInput` in `docs/openapi.yaml`, the same as `POST /admin/venues`.

| Column | Required | Rule |
| --- | --- | --- |
| `sourceKey` | no | The venue's ID in your source, at most 200 characters. See "Matching". |
| `name` | yes | 1 to 300 characters, one line |
| `shortName` | no | The acronym, at most 40 characters |
| `type` | yes | `conference`, `journal`, `workshop` or `special_issue` |
| `fields` | yes | 1 to 5 keys of `GET /api/v1/venues/fields` (the list is below) |
| `attendance` | conference and workshop | `in_person`, `online` or `hybrid`; empty for a journal or special issue |
| `city` | in person or hybrid | At most 100 characters; empty when online, or for a journal or special issue |
| `country` | in person or hybrid | ISO 3166-1 alpha-2, such as `BD` (letter case is ignored) |
| `startsOn`, `endsOn` | no | `YYYY-MM-DD`; conference and workshop only; the end is not before the start |
| `timeZone` | yes | `AoE`, `UTC` or an IANA name such as `Asia/Dhaka` |
| `submissionDeadline` | yes | A local time in `timeZone`: `YYYY-MM-DD`, `YYYY-MM-DDTHH:MM` or `YYYY-MM-DDTHH:MM:SS`. A date alone means 23:59:59 that day |
| `abstractDeadline` | no | Same format and zone; not after the submission deadline |
| `notificationAt` | no | Same format and zone; not before the submission deadline |
| `officialUrl` | yes | `https://` only, with a host and no user name, at most 2,048 characters |
| `scope` | yes | 1 to 10,000 characters. Plain text or a little Markdown (`**bold**`, lists); the app always shows it as text, never as HTML. Tabs and line breaks are the only control characters allowed |

**AoE** ("anywhere on Earth") is UTC−12 all year. A deadline of `2027-03-01` AoE ends at
2027-03-02 11:59:59 UTC, which is 17:59:59 on 2 March in Dhaka. The app stores the instant and
the label, and shows both.

## Matching

Each row's key is its `sourceKey`. When that is empty, the key is made from the name and a
year: `auto:` + the name in lower case with runs of spaces made one (accents are kept) + `:` +
the year of `startsOn`, or of the submission deadline when there is no `startsOn`. For example,
`Journal of Floods` with a 2027 deadline gets `auto:journal of floods:2027`, so next year's call
becomes a new venue.

- No imported venue has the key: the row is **created**.
- A live imported venue has it, and some value differs: it is **updated** in place, never
  duplicated.
- A live imported venue has it, and nothing differs: **unchanged**.
- A venue an administrator added by hand is never matched, even with the same name.
- The venue with that key was removed: the row is **rejected** with `venue_removed`. An import
  never brings a removed venue back.
- A second row with a key an earlier row of the same file has: **rejected** with
  `duplicate_in_file`.

A rejected row is skipped and the others are still written. The answer lists every row with its
outcome, the venue's ID (except for a row created on a dry run) and, for a rejected row, every
reason as `{field, code}`. The codes are listed under `VenueImportReason` in the API contract.

Every accepted row of one file is written in one transaction, each with an `import_create` or
`import_update` row in the venue audit trail (`venue_actions`) sharing one import ID. An update
records the names of the changed fields, never their values. Two imports take turns.

## Research fields

| Key | Label |
| --- | --- |
| `medical_imaging` | Medical imaging |
| `explainable_ai` | Artificial intelligence and machine learning |
| `bangla_nlp` | Natural language processing |
| `climate_modelling` | Climate science |
| `public_health_epidemiology` | Public health and epidemiology |
| `materials_science` | Materials science |
| `robotics` | Robotics and automation |
| `agri_remote_sensing` | Agriculture and remote sensing |
| `renewable_energy` | Energy and power systems |
| `water_resources` | Water resources and hydrology |
| `cybersecurity` | Cybersecurity and privacy |
| `genomics_bioinformatics` | Genomics and bioinformatics |
| `development_economics` | Economics and development |
| `education_technology` | Education and learning technology |
| `structural_engineering` | Civil and structural engineering |
| `urban_transport` | Transportation and urban planning |
| `marine_fisheries` | Marine science and fisheries |
| `wireless_networks` | Networks and communications |
| `drug_discovery` | Pharmaceutical sciences |
| `mental_health` | Psychology and mental health |
| `other` | Other |

## Example (CSV)

```csv
sourceKey,name,shortName,type,fields,attendance,city,country,startsOn,endsOn,timeZone,submissionDeadline,officialUrl,scope
cfp-1,Conference on Robots,COR,conference,robotics;explainable_ai,hybrid,Dhaka,BD,2027-06-01,2027-06-03,AoE,2027-03-01,https://cor.example.org,"Robots and their learning.
**Topics**: planning, control."
,Journal of Floods,,journal,water_resources,,,,,,Asia/Dhaka,2027-04-01T17:00,https://jof.example.org,Floods.
```
