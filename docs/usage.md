# Go Pusher Usage Guide

This guide is for app developers using Go Pusher. It covers publishing events
from a backend and subscribing to public, private, and presence channels from a
browser.

## Before You Start

Register an app at <http://localhost:8080> and keep these values:

- `APP_ID`: identifies your app.
- `APP_KEY`: used by browser clients to connect.
- `APP_SECRET`: private backend credential. **Never put it in browser code.**

Run the Go server with a channel signing key configured:

```sh
export CHANNEL_AUTH_SIGNING_KEY="your-long-random-signing-key"
go run .
```

Use `https`/`wss` in production. The examples below use local development URLs.

## Publish an Event

Publish from your backend to:

```text
POST /apps/{APP_ID}/events
Authorization: Bearer {APP_SECRET}
Content-Type: application/json
```

JSON body:

```json
{
  "topic": "notifications",
  "event": "message",
  "payload": {"text": "Hello!"}
}
```

Python example:

```python
import os
import requests

PUSHER_URL = os.getenv("PUSHER_URL", "http://localhost:8080")
APP_ID = os.environ["APP_ID"]
APP_SECRET = os.environ["APP_SECRET"]

response = requests.post(
    f"{PUSHER_URL}/apps/{APP_ID}/events",
    headers={"Authorization": f"Bearer {APP_SECRET}"},
    json={
        "topic": "notifications",
        "event": "message",
        "payload": {"text": "Hello from Python!"},
    },
    timeout=5,
)
response.raise_for_status()
print(response.json())
```

A successful response looks like:

```json
{"event":"message","status":"published","topic":"notifications"}
```

The `topic` is the channel name. Publishing to a private or presence channel
uses the same endpoint and backend secret as publishing to a public channel.

## Authorize a Private or Presence Channel

The Go authorization endpoint is:

```text
POST /apps/{APP_ID}/private-channel-auth
Authorization: Bearer {APP_SECRET}
Content-Type: application/json
```

The browser first connects to the WebSocket and receives a `socket_id`. It then
asks **your backend** to authorize that socket and channel. Your backend must
check the signed-in user's access before calling Go. Do not call this Go
endpoint directly from browser JavaScript: it requires `APP_SECRET`.

### Private-channel request

```json
{
  "socket_id": "socket-id-from-websocket",
  "channel": "private-orders"
}
```

The response contains a short-lived authorization:

```json
{"auth":"signed-authorization-token"}
```

### Presence-channel request

For `presence-*` channels, your backend also provides the signed-in user's
identity and the member information safe to share with channel participants:

```json
{
  "socket_id": "socket-id-from-websocket",
  "channel": "presence-room-1",
  "channel_data": {
    "user_id": "user-42",
    "user_info": {"name": "Ada"}
  }
}
```

Build `user_id` from your authenticated user record; never trust a user ID sent
by the browser. Keep `user_info` small (maximum 4 KiB) and include only fields
that other members may see. The response includes both `auth` and
`channel_data`:

```json
{
  "auth": "signed-authorization-token",
  "channel_data": {
    "user_id": "user-42",
    "user_info": {"name": "Ada"}
  }
}
```

Python helper for calling Go from your backend:

```python
import requests


def request_channel_authorization(pusher_url, app_id, app_secret, socket_id, channel, channel_data=None):
    body = {"socket_id": socket_id, "channel": channel}
    if channel_data is not None:
        body["channel_data"] = channel_data

    response = requests.post(
        f"{pusher_url}/apps/{app_id}/private-channel-auth",
        headers={"Authorization": f"Bearer {app_secret}"},
        json=body,
        timeout=5,
    )
    response.raise_for_status()
    return response.json()
```

Call this helper only after your backend authenticates the user and decides they
may access the requested channel. For a presence channel, pass identity data
from that authenticated user, for example:

```python
channel_data = {
    "user_id": str(current_user.id),
    "user_info": {"name": current_user.display_name},
}
```

## Connect and Subscribe from JavaScript

Connect with the app ID and public app key:

```js
const appId = "YOUR_APP_ID";
const appKey = "YOUR_APP_KEY";
const pusherUrl = "http://localhost:8080";
const wsUrl = new URL(pusherUrl);
wsUrl.protocol = wsUrl.protocol === "https:" ? "wss:" : "ws:";

const socket = new WebSocket(
  `${wsUrl.origin}/apps/${encodeURIComponent(appId)}/ws?key=${encodeURIComponent(appKey)}`
);
```

Go sends a connection message when the socket is ready:

```json
{"event":"connected","socket_id":"..."}
```

Use that `socket_id` when asking your backend to authorize a private or
presence channel. Then send a subscribe command over the same WebSocket.

### Public channel

Public channels do not need an authorization token:

```js
socket.send(JSON.stringify({
  action: "subscribe",
  channel: "notifications",
}));
```

### Private channel

Ask your backend for authorization, then send the returned `auth` value:

```js
async function subscribePrivate(socket, socketId, channel) {
  const response = await fetch("/your-backend/channel-auth", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ socket_id: socketId, channel }),
    credentials: "same-origin",
  });
  if (!response.ok) throw new Error("Channel authorization failed");
  const { auth } = await response.json();

  socket.send(JSON.stringify({ action: "subscribe", channel, auth }));
}
```

The `/your-backend/channel-auth` route is part of your application. It should
check the user's login and permissions, then call the Go authorization endpoint
from the backend using `APP_SECRET`.

### Presence channel

Presence uses the same authorization request flow. The backend supplies
`channel_data`; send the returned `auth` in the WebSocket command. Identity is
already covered by the signed authorization, so do not add `channel_data` to the
subscribe command:

```js
async function subscribePresence(socket, socketId, channel) {
  const response = await fetch("/your-backend/channel-auth", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ socket_id: socketId, channel }),
    credentials: "same-origin",
  });
  if (!response.ok) throw new Error("Channel authorization failed");
  const { auth } = await response.json();

  socket.send(JSON.stringify({ action: "subscribe", channel, auth }));
}

// Example channel name:
subscribePresence(socket, socketId, "presence-room-1");
```

Your backend knows the current user from its login session and adds that user's
`channel_data` when it calls Go. The browser must not choose or override the
presence identity.

## Channel Types

| Type | Channel name | Subscribe authorization | Member information |
|---|---|---|---|
| Public | `notifications` | None beyond app key at WebSocket connection | Not tracked |
| Private | `private-orders` | Required; request a grant from your backend | Not shared |
| Presence | `presence-room-1` | Required; request a grant from your backend | Shared `user_id` and `user_info` |

Channel names are case-sensitive. Use the matching prefix for protected
channels: `private-` or `presence-`. Any other channel name is treated as
public.

## Listen for Messages and Presence Events

Messages arrive over the WebSocket as JSON with `channel`, `event`, and `data`:

```js
socket.addEventListener("message", (messageEvent) => {
  const message = JSON.parse(messageEvent.data);
  console.log(message.channel, message.event, message.data);
});
```

A successful subscription sends `subscription_succeeded`. For a presence
channel, `data.presence` contains the current unique member count and member
list:

```json
{
  "channel": "presence-room-1",
  "event": "subscription_succeeded",
  "data": {
    "presence": {
      "count": 2,
      "members": [
        {"user_id":"user-42","user_info":{"name":"Ada"}},
        {"user_id":"user-51","user_info":{"name":"Lin"}}
      ]
    }
  }
}
```

When a new user joins, existing members receive `member_added`. When the user's
last connection leaves, remaining members receive `member_removed`. Each event's
`data` contains that member's `user_id` and `user_info`.

An authorization failure sends a `subscription_error` event to the requesting
socket. For regular public/private messages, `data` is the `payload` supplied
when publishing.

## Common Errors

- `401 Invalid app key`: check `APP_ID` and `APP_KEY` in the WebSocket URL.
- `401 Invalid app credentials`: check the backend's `APP_SECRET`.
- `401 socket is not connected`: request authorization with the socket ID from
  the same live WebSocket connection.
- `400`: check the channel prefix and, for presence, include a non-empty
  `channel_data.user_id`.
- `503 private-channel authentication is not configured`: start Go with
  `CHANNEL_AUTH_SIGNING_KEY` set.
- `subscription_error`: the grant may be missing, expired, for another channel,
  or for another socket.

For the project-specific Flask demo, see the setup steps in [README.md](README.md).
