# Sync contract

The real-time editing connection between the web client and the sync server (`sync/`,
slice 22): FR-12.4, FR-12.5, FR-12.7 to FR-12.9, NFR-2, NFR-10.1, NFR-12. `docs/openapi.yaml`
holds the ticket operation and the API's internal operations; this file is the contract for
what happens on the WebSocket. Decisions and reasons: `docs/decisions.md`, 2026-11-19.

The sync server speaks the Hocuspocus protocol (`@hocuspocus/server` 4.8, Yjs 13.6). The web
client uses `@hocuspocus/provider` with the same major version.

## Connecting

1. Ask the API for a ticket: `POST /api/v1/projects/{projectId}/files/{fileId}/collab-ticket`
   (members only; text files only). The answer holds `ticket` (works once, within 30 s),
   `role`, `displayName` and `epoch`.
2. Open a provider on the sync server with the file's ID as the document name:

   | Where | URL |
   | --- | --- |
   | Development | `ws://localhost:8091` (`make sync`) |
   | Production | `wss://<domain>/sync` (reverse proxy, `docs/deploy.md`) |

   The provider's `token` is the ticket, or `<ticket>.<epoch>` when the client holds edits it
   made offline in that epoch (see Epochs). Give the provider a token **function** that asks for a
   new ticket each time: a ticket works once, and the provider sends the token again on every
   reconnect.
3. The browser's `Origin` must be one the server allows: `APP_BASE_URL`'s, those in
   `SYNC_ALLOWED_ORIGINS` and, unless `APP_ENV=production`, `http://localhost:3000`, `:3001`,
   `http://127.0.0.1:3000` and `:3001`. The server logs each origin it refuses, and its
   `listening` line lists the allowed ones.

One WebSocket may carry several documents (one provider per open file on a shared
`HocuspocusProviderWebsocket`). Each document is admitted, refused and closed on its own.

## Messages from the server

The server sends three kinds of **stateless** message, told apart by `type`: `session`,
`presence` and `stored`. A client ignores any other type. After the document syncs, the server
sends `session`, and sends it again whenever the connection's rights change:

```json
{"type": "session", "role": "editor", "readOnly": null, "maxEditors": 4, "colour": "#0B7A75", "epoch": "0193..."}
```

| Field | Meaning |
| --- | --- |
| `role` | The member's role now: `owner`, `editor` or `viewer` |
| `readOnly` | `null`: may edit. `"viewer"`: the role allows reading only (FR-12.7). `"capacity"`: `maxEditors` members already edit this file; this member edits as soon as one leaves (FR-12.8) |
| `maxEditors` | How many members may edit one file at once (`SYNC_MAX_EDITORS_PER_FILE`, default 4), in every session message. The client builds the reason from it: "This file already has {maxEditors} people editing. You can read it and will be able to edit when someone leaves." |
| `colour` | This member's cursor colour in this file, distinct from the others' while there are 8 or fewer people (FR-12.5) |
| `epoch` | The document's Yjs history (see Epochs); keep it with any offline state |

When `readOnly` turns `null` again the server asks the client for what it has (a sync step 1),
so a client that kept edits it made while read-only sends them then. The client should not let
a member type while `readOnly` is set: what a read-only connection sends is dropped (and
logged), and its later edits wait behind the dropped ones until the connection may edit.

**Presence** of the project (FR-12.5), sent to every connection to one of the project's files
whenever someone opens or leaves one of them, or their rights change (changes within 50 ms go
out as one message):

```json
{"type": "presence", "files": {"<fileId>": [{"id": "<userId>", "name": "Ada Lovelace", "colour": "#0B7A75", "readOnly": null}]}}
```

`files` lists the project's files that members have open now, each with the members who may
edit it, earliest first: `readOnly` is `null` (editing) or `"capacity"` (waiting for a slot).
Viewers are never listed, and no other project's files ever are. It goes only to connections of
users whose last role check (the ticket, an event or the 60 s check) found them members: a
removed member's connections are closed and get none after that. `colour` is the member's
colour in that file. Each message replaces the previous one.

**Stored** (NFR-10.1), sent to every connection to the document after a store that leaves
nothing unsaved:

```json
{"type": "stored", "version": 12}
```

Everything the server had applied before the store is in the database. A store that changes
arrived during sends nothing; the next one does. A client shows "Saved" when it gets this while
it has no change the server has not acknowledged, and "Live" from its next change on.

**Awareness** (cursors and names) is relayed as Yjs awareness. The server overwrites the `user`
field of every state a connection sends with `{id, name, colour}` from the ticket, so a client
cannot show another name or colour.

## Refusals and closes

A refused connection gets the provider's `authenticationFailed` with a `reason`; a connection
the server ends gets a close message for that document with a `reason` (the provider's `close`
event). The codes are what the server logs.

| Reason | Code | When | The client should |
| --- | --- | --- | --- |
| `ticket_invalid` | | Unknown, expired, spent, for another file, or the holder has no access now | Get a new ticket once; if the ticket request itself is refused (404), the member has no access |
| `origin_not_allowed` | | `Origin` is not an allowed one | Use REST (a misconfiguration; the server logged the origin) |
| `too_many_connections` | | The user already has 10 documents open on this server | Use REST; close files it does not show |
| `epoch_mismatch` | | The token named an epoch the document no longer has | See Epochs |
| `unavailable` | | The API or Redis did not answer | Retry with backoff; meanwhile use REST (below) |
| `access_ended` | 4403 | Removed from the project, or found suspended or unverified at a check | Close the editor; REST will answer 404 |
| `session_ended` | 4401 | Logout, password change or reset, account deletion, a reused refresh token, or suspension | Refresh the session, then get a new ticket; stop if the refresh fails |
| `file_gone` | 4404 | The file or its project was deleted | Close the file and reload the tree |
| `document_too_large` | 4413 | An edit would take the text over 1 MiB of UTF-8 | Say so; undo the last edit locally |
| `rate_limited` | 4429 | More than 120 messages at once or 60 a second, sustained | Reconnect after a moment |
| `reload` | 4409 | The file was changed over REST while the sync server held it (only possible while Redis was down) | Treat the local text as an offline copy in another epoch (see Epochs) |
| `shutting_down` | 1001 | The server is stopping; everything it received is stored | Reconnect with backoff (a new ticket) |

A WebSocket message over 4 MiB closes the whole socket with code 1009.

## Epochs and offline edits (FR-12.9)

The Yjs history of a file has a name, its **epoch** (a UUID). Yjs merges a client's offline
updates only into the history they were made in; merged into another, they would duplicate
text. The sync server starts a new epoch whenever it builds the document from the plain text:
for a new file, and after a REST save (which clears the stored Yjs state). It stores a new epoch
at once, so a restart keeps it.

A client that kept a document while offline (in memory or in IndexedDB) connects with
`<ticket>.<epoch>`. If the document still has that epoch, the provider syncs and both sides
merge (the server applies the client's updates only if the member may still edit). If not, the
connection is refused with `epoch_mismatch` before any data is exchanged: the client then shows
its local text as a conflict (the 21.2 dialog: use the server's, or overwrite with mine through
the editor) and connects without an epoch.

A client that holds no offline state sends the ticket alone.

## REST saves while a file is open

While the sync server holds a file's document it keeps the Redis key `sync:live:<fileId>` (TTL
30 s, renewed every 10 s, set before it reads the file). `PUT /files/{fileId}/content` then
answers 409 `live_session_open`, and 503 `live_check_unavailable` when Redis does not answer.
**The web client edits text files through the sync connection and uses the REST save only when
live editing is refused or the sync server cannot be reached** (showing "Live editing
unavailable, saving normally", with the reason in development). Only `access_ended`,
`session_ended` that cannot be refreshed and `file_gone` close the file; every other refusal,
a ticket the API refuses and a sync server that does not answer open it over REST, as long as
the API answers, and live editing is tried again in the background. That
REST save clears the Yjs state, so the next sync session starts a new epoch.

## Saving (NFR-10.1)

The sync server stores the document through the API (one UPDATE: Yjs state, epoch, plain text,
version + 1, last editor, time) 2 s after the last change, and at the latest 4 s after the first
unsaved one, so every edit is in the database within 5 s; at once when the last member leaves;
and on SIGTERM. A store that fails keeps the document in memory and is tried again (1 s, 2 s, 4 s
... 30 s); `/health` says `degraded` meanwhile. The provider counts an edit as synced when the
server has applied it, not stored it: if the server crashed in the 4 s before a store, the
members' clients still hold the edit and send it again when they reconnect.

## Events from the API

The API publishes on Redis channel `sync:<db>:events` after each commit (at most once):

| `kind` | Fields | The sync server |
| --- | --- | --- |
| `members_changed` | `projectId`, `userIds` | Reads those users' roles again: removed users are closed (`access_ended`), viewers made read-only in place, editors allowed again if a slot is free |
| `files_deleted` | `projectId`, `fileIds` | Closes those documents (`file_gone`) |
| `project_deleted` | `projectId` | Closes the project's documents (`file_gone`) |
| `user_ended` | `userIds` | Closes the user's connections (`session_ended`) |
| `flush` | `projectId`, `requestId` | Stores every document of the project with changes not yet stored, now, then answers (below) |

### Flush (slice 23)

A compile asks for `flush` before it reads the project's files, so it compiles what members
have typed and not only what was stored (FR-13.1; `docs/compile.md`). The API sends it only
when a text file of the project has a live mark (`sync:live:<fileId>`), and only when a sync
server subscribes to the channel. `requestId` is 32 hexadecimal characters; anything else is
ignored with a warning.

The sync server stores each document of the project that has unsaved changes at once (after a
store already running for it), then pushes its answer onto the list
`sync:flushack:<requestId>` (expiring after 30 s): `ok` when every such document is stored,
`failed` when one could not be. The API waits on that list with `BLPOP` for at most 5 s. An
edit that arrives after the request may or may not be in the compile. Without an answer the
compile reads the stored text and the compile says so (`liveTextConfirmed: false`). The API
waits on a connection of its own: a `BLPOP` on the client the server also uses would hold up
the answer.

Every 60 s the server also reads the role of every open connection again (one call per 200),
so a lost event or a suspension that took effect without one is caught within a minute.

## Limits

| Limit | Value |
| --- | --- |
| Editors per file (a user's tabs count once; viewers do not count) | 4 (`SYNC_MAX_EDITORS_PER_FILE`, 1 to 8) |
| Document text | 1 MiB of UTF-8 (the API's limit); Yjs state stored up to 8 MiB |
| WebSocket message | 4 MiB |
| Messages per connection | 60 a second, bursts of 120 |
| Open documents per user per server | 10 |
| Ticket lifetime | 30 s, one use |
| Idle connection | closed after 60 s without a message; the provider's awareness renews every 15 s, so keep awareness on |

## Health

`GET /health` on the sync server: 200 `{"status":"ok", ...}`, or 503 with `status: degraded`
when Redis or the API does not answer or a document could not be stored. The body also gives
the number of open documents and connections.

## Scaling

One instance serves NFR-12 (10 files x 4 editors) with room to spare (`docs/evaluation.md`).
More instances would need: routing by document so one file's members meet on one instance
(or Hocuspocus's Redis extension to share updates between instances), the editor count and the
per-user connection count in Redis instead of memory, and the live mark renewed by whichever
instance holds the document. None of this is built.
