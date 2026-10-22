# CLAUDE.md

All project rules live in AGENTS.md. Read it first.

@AGENTS.md

## Working style
- Work one chunk at a time. Do only what the current prompt asks.
- Plan first. If the SRS (docs/SRS.md), docs/decisions.md and the
  prompt disagree, stop and ask.
- Ask before adding any new dependency, and say why.
- Run make lint and make test (backend) or lint, typecheck and
  tests (frontend) before reporting.
- Do not commit; the developer commits after reviewing.
