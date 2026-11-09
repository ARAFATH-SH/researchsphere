# Real-time contract

The WebSocket between the web client and the API server (FR-16.1, FR-16.3, FR-16.4, FR-9.4).
`docs/openapi.yaml` describes the upgrade and the payload schemas (`Realtime*`); OpenAPI cannot
describe what follows the upgrade, so this file is the contract for that.

The socket only pushes events and carries typing. Sending messages, read markers, accept,
decline, block and report stay REST. Delivery is at most once, and REST stays the truth.

## Connecting

`GET /api/v1/ws` upgrades to WebSocket.

- Authentication is the `rs_access` cookie at the handshake, and the platform action "View
  profiles, ..." (SRS 2.3).
- The `Origin` header must equal the origin of `APP_BASE_URL`. A missing one is refused.
- When the upgrade answers 101, the user's event channel is already live.
- The browser connects to this URL directly, not through the Next.js rewrite. The address comes
  from `NEXT_PUBLIC_WS_URL`: `ws://localhost:8080/api/v1/ws` in development, and in production
  `wss://<domain>/api/v1/ws` on the web client's own domain.

| Status | `code` | Meaning |
| --- | --- | --- |
| 101 | | Connected |
| 401 | `unauthenticated` | No valid access token: refresh, then try again |
| 403 | `origin_not_allowed` | The `Origin` is not the web client's |
| 403 | `account_suspended` | Suspended, with `reason` and `until` |
| 426 | `upgrade_required` | Not a WebSocket request |
| 503 | `realtime_unavailable` | Real-time delivery is down. REST works: reconnect with backoff |

A browser's `WebSocket` does not show the status of a refused handshake. A client that fails
to connect should refresh and try again with backoff, and go on polling meanwhile.

## What a client must do

- **After every reconnect, refetch conversations, unread counts and presence.** Events are not
  stored: anything sent while the socket was closed is gone.
- **While a conversation is open, refresh presence every 60 s** with `GET /api/v1/presence`.
  This covers a crashed API instance, which cannot send `online: false`.
- Keep the 60 s poll of the notification and message counts as the fallback while the socket is
  down, and stop it while connected, as events carry them (FR-9.4). The connection-request count
  has no event, so it is polled either way.
- Treat events as idempotent: `presence` can repeat, and the sending tab receives its own
  `message.created`, which it matches by `clientId`.
- Clear a typing indicator on `conversation.updated`, and by itself if no `isTyping: false`
  arrives within 6 s of the last start.
- On `conversation.members`, refetch the group (`GET /conversations/{id}`) for its members and
  roles. On `conversation.updated` with `accessLost: true`, close the thread and drop the
  conversation from the lists: the user left the group or was removed, and every read of it now
  answers 404.

## Close codes

| Code | Meaning | The client should |
| --- | --- | --- |
| 1001 | The server is shutting down | Reconnect with backoff |
| 1009 | A frame was over 4 KB | Reconnect, and not resend that frame |
| 1013 | The send buffer filled up, or the connection died | Reconnect with backoff, then refetch |
| 4401 | The access token expired | Refresh, then reconnect |
| 4403 | The session ended: logout on any device, suspension, password change or reset, account deleted | Refresh. If that works, reconnect; if it is refused, the user is signed out |
| 4409 | Replaced by a newer socket: the user has more than 5 | **Not reconnect** until the person shows the tab again, or tabs would keep closing each other. Go on polling |

An access token lasts 15 minutes, so expect 4401 about that often. A socket cannot take a new
token; the client reconnects with the refreshed cookie.

Every tab holds the same cookie, so all of a browser's sockets get 4401 at the same moment.
Tabs must take turns refreshing: a retired refresh token ends the session at once
(`docs/decisions.md`, 2026-10-13).

## Limits

| Limit | Value |
| --- | --- |
| Ping from the server | Every 25 s; no pong within 10 s drops the connection |
| Sockets per user | 5 across all instances; the oldest is closed with 4409 |
| Incoming frame | 4 KB; a larger one closes with 1009 |
| Incoming events | 20 per 10 s per socket; the rest are ignored |
| Send buffer | 32 frames; a client that fills it is closed with 1013 and delays nobody |

## Events

Each frame is JSON text with `type` and `data`.

### Server to client

| `type` | Sent to | `data` |
| --- | --- | --- |
| `message.created` | The other member (in a group, every other current member), and the sender's own tabs | `RealtimeMessageCreated`: `{message, conversationState, clientId?}` |
| `message.updated` | The other member (in a group, every other current member), and the sender's own tabs | `RealtimeMessageUpdated`: `{message}`, the REST `Message` with the new `body` and `editedAt` |
| `message.deleted` | The other member (in a group, every other current member), and the sender's own tabs | `RealtimeMessageDeleted`: `{conversationId, messageId}` |
| `attachment.updated` | The uploader's tabs; once the file is sent, every current member who joined by the message | `RealtimeAttachmentUpdated`: `{conversationId, messageId, attachmentId, status}` |
| `conversation.read` | The other member (in a group, every other current member), when a read marker moves | `{conversationId, messageId}`; in a group, refetch `readers` for "Seen by" |
| `conversation.updated` | Both members; in a group, a member who left or was removed | `{conversationId, state, canMessage, accessLost}`, each their own view |
| `conversation.members` | Every current member of a group, after a change | `RealtimeConversationMembers`: `{conversationId, change, actorId, userIds, name}` |
| `unread.changed` | The user whose counts may have changed | `{total, requests}` |
| `typing` | The other member; in a group, every other member with no block either way | `{conversationId, userId, isTyping}` |
| `presence` | Allowed viewers | `{userId, online}` |
| `notification.created` | The recipient | `Notification`, as the list shows it |
| `notifications.unread` | The recipient | `{count}` |

`clientId` appears only in the copy sent to the sender. `message` is the REST `Message`.

```json
{"type": "message.created", "data": {
  "message": {"id": "...", "conversationId": "...", "senderId": "...", "body": "Hello", "createdAt": "2026-11-05T12:00:00Z"},
  "conversationState": "active",
  "clientId": "..."
}}
```

`message.updated` follows an edit (FR-16.5) and `message.deleted` a deletion. Both go out after
the commit, to every tab of both members, the editing tab included. An unchanged edit and a
second deletion send nothing. Replace or mark the message in place, keeping its position; ignore
one that is not loaded. A deleted message keeps its place with `deleted: true` and a null
`body`; the event carries no text, so a client that missed it learns of the deletion on its next
fetch.

```json
{"type": "message.updated", "data": {
  "message": {"id": "...", "conversationId": "...", "senderId": "...", "body": "Hello again",
              "createdAt": "2026-11-05T12:00:00Z", "editedAt": "2026-11-05T12:03:00Z", "deleted": false}
}}
{"type": "message.deleted", "data": {"conversationId": "...", "messageId": "..."}}
```

`attachment.updated` says an attachment's malware scan finished (FR-16.7): `status` is `clean`,
`infected` or `failed`, never `pending`. It goes out after the commit. Before the file is sent,
`messageId` is null and only the uploader's tabs get it, so the composer can show the result.
After it is sent, every current member who can see the message gets it, as in the thread (a
member who joined later does not); for a deleted message only the uploader. Update the file's
status in place and fetch nothing. `infected` and `failed` are final: show the file as removed
or unavailable. A client that missed the event sees the status in the message on its next fetch;
a download before `clean` answers 409 or 410 (`docs/openapi.yaml`).

```json
{"type": "attachment.updated", "data": {
  "conversationId": "...", "messageId": "...", "attachmentId": "...", "status": "clean"
}}
```

`message.created`, `message.updated` and the REST `Message` carry `attachments`, each
`{id, name, size, contentType, status}`; a message may have files and no `body`.

`conversation.updated` follows a request being accepted or declined, a connection activating a
request, a block or unblock, and the suspension or restoration of either member. It does not
say why, so a blocked person is not told about a block. `accessLost` is false except in one
case: a member who leaves a group or is removed from it gets `{state: "active", canMessage:
false, accessLost: true}` on every tab, followed by `unread.changed`.

### Groups (FR-16.6)

Every change to a group writes a system message (`Message.kind: "system"`, with `event`) and
sends it as `message.created` to the members after the change, without `clientId` and with no
`unread.changed`, as system messages never count as unread. Then `conversation.members` goes to
the same people:

| `change` | After | `actorId` | `userIds` |
| --- | --- | --- | --- |
| `added` | A create, or the owner adding members; the new members get it too | The owner | The members added |
| `removed` | The owner removing a member | The owner | The one removed |
| `left` | A member leaving | Who left | Who left |
| `owner_changed` | The owner's membership ending (leaving or account deleted) | null | The new owner |
| `renamed` | The owner renaming the group | The owner | Empty |

`name` is the group's name after the change. When the owner leaves, `left` comes first, then
`owner_changed`, matching the order of the two system messages. The user who is gone gets
neither: they get `conversation.updated` with `accessLost: true`. An ownership hand-over that
follows an account deletion writes its system message but sends no event; open pages see it on
their next fetch.

A message in a group goes to every member, also between two members who have blocked each
other: a block removes nobody from a group. Delivery costs one unread count per recipient, up to
49 per message (`docs/decisions.md`, 2026-11-09).

`unread.changed` follows a message, a deleted message (it no longer counts as unread), a read marker, an answered request, a block that declines a
request, and the suspension or restoration of a conversation partner.

### Client to server

| `type` | `data` |
| --- | --- |
| `typing.start` | `{conversationId}` |
| `typing.stop` | `{conversationId}` |

Anything else is ignored and logged at debug level, without its content. No error comes back.

## Typing

- Relayed only to the other member, only if both are members, the conversation is active, the
  other member is available and neither blocked the other. A refused event gets no answer.
- In a group, relayed to every other current member, except those in a block with the typer
  either way. `userId` says who is typing. A former member's typing goes nowhere.
- At most one `typing.start` per 3 s per conversation; the rest are dropped.
- 6 s after the last relayed start the server sends `isTyping: false` by itself.
- Nothing is stored.

## Presence

Online means at least one open socket. An entry lives in Redis for 60 s, refreshed by the
heartbeat, so a crashed instance does not leave people online.

A person's presence is shown only to someone who shares an active direct conversation with them,
with no block either way. Sharing a group does not count: group members are connections of the
creator, not necessarily of each other. `GET /api/v1/presence?userIds=a,b` (at most 50) answers every ID; anyone the
caller may not see reads `online: false`, so the answer says nothing about who may be seen.

`online: false` follows 5 s after a user's last socket closes, so the reconnect the token forces
every 15 minutes does not flicker. It goes out at once on a server shutdown. A block sends
`online: false` to both people, though neither went offline.
