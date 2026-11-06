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
| `message.created` | The other member, and the sender's own tabs | `RealtimeMessageCreated`: `{message, conversationState, clientId?}` |
| `conversation.read` | The other member, when a read marker moves | `{conversationId, messageId}` |
| `conversation.updated` | Both members | `{conversationId, state, canMessage}`, each their own view |
| `unread.changed` | The user whose counts may have changed | `{total, requests}` |
| `typing` | The other member | `{conversationId, userId, isTyping}` |
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

`conversation.updated` follows a request being accepted or declined, a connection activating a
request, a block or unblock, and the suspension or restoration of either member. It does not
say why, so a blocked person is not told about a block.

`unread.changed` follows a message, a read marker, an answered request, a block that declines a
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
- At most one `typing.start` per 3 s per conversation; the rest are dropped.
- 6 s after the last relayed start the server sends `isTyping: false` by itself.
- Nothing is stored.

## Presence

Online means at least one open socket. An entry lives in Redis for 60 s, refreshed by the
heartbeat, so a crashed instance does not leave people online.

A person's presence is shown only to someone who shares an active conversation with them, with
no block either way. `GET /api/v1/presence?userIds=a,b` (at most 50) answers every ID; anyone the
caller may not see reads `online: false`, so the answer says nothing about who may be seen.

`online: false` follows 5 s after a user's last socket closes, so the reconnect the token forces
every 15 minutes does not flicker. It goes out at once on a server shutdown. A block sends
`online: false` to both people, though neither went offline.
