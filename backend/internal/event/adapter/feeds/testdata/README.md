# Recorded fixtures of the venue import's sources

Files recorded on 2026-11-24 so the tests never reach the network (slice 25A,
`docs/venues-sync.md`). They are copies, unchanged except where noted.

| Folder | Source | Commit or query | Licence |
| --- | --- | --- | --- |
| `ccf/conference/` | https://github.com/ccfddl/ccf-deadlines | `193d48d5d3c112ab4743e365451aae36303a4d91` | MIT, Copyright (c) 2021 CCFDDL |
| `hci/conferences/` | https://github.com/hci-deadlines/conf-database | `e6af4d6d3152d22fb2e04d6466af865c07273dc9` | MIT |
| `openalex/sources-page1.json` | https://api.openalex.org/sources, 6 journals, `select` limited to the fields the feed reads; each journal's topics trimmed to 5 and their objects to id, name, subfield and field | | CC0 |

The MIT licence of both repositories: "Permission is hereby granted, free of charge, to any
person obtaining a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
the Software, and to permit persons to whom the Software is furnished to do so, subject to
the following conditions: The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software. THE SOFTWARE IS PROVIDED "AS
IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED."
