# App, Channel, and Event Behavior

This guide describes what a user of the Go service and Flask example can
expect. It focuses on observable behavior, not the internal implementation.

## Create an app

Open <http://localhost:8080> and submit an app name, or send:

```sh
curl -X POST http://localhost:8080/apps \
  -H "Content-Type: application/json" \
  -d '{"name":"my-app"}'
```

The response contains an app `id`, public `key`, and private `secret`. Each is
randomly generated when that app name is first registered. Their displayed
formats are `app_` + 48 hexadecimal characters, `key_` + 48 hexadecimal
characters, and `secret_` + 48 hexadecimal characters. Registering the same
name again returns the existing app and credentials instead of creating a
second app. A missing or blank name returns `400`.

Keep the response private: the app secret is a backend credential. In this
project it is returned by app registration and the app database, so protect the
database and its backups too. The app key is public and identifies the app; it
does not authorize a user to read a private channel.

## Configure the Flask example

Copy `clients/flask_app/.env.example` to `.env` and fill in the credentials and
URLs:

```dotenv
GO_PUSHER_URL=http://localhost:8080
FLASK_SECRET_KEY=use-a-random-session-signing-key
USER_DB_PATH=data/flask_users.db
APP_ID=app-id-from-registration
APP_KEY=key-from-registration
APP_SECRET=secret-from-registration
```

Flask uses `APP_ID` and `APP_KEY` in the browser to open a WebSocket. The
browser does not receive `APP_SECRET`; Flask uses it in server-to-server HTTP
requests to Go when publishing or requesting private-channel authorization.
`FLASK_SECRET_KEY` protects Flask login sessions and is separate from the Go
app credentials. Restart Flask after changing its environment.

## Connect and subscribe

The browser connects to `/apps/{app_id}/ws?key={app_key}`. With a valid key, Go
opens the WebSocket and sends a `connected` event containing a `socket_id`.
That ID belongs to this connection only. An invalid app ID or key is rejected
with HTTP `401` during the WebSocket handshake.

For a public channel, the browser sends a `subscribe` command with the channel
name. Public names do not need a prefix. For a private channel, the name must
start with `private-`; the browser first asks Flask to authorize the current
connection, then sends the returned grant with its `subscribe` command.

The Flask example requires a logged-in session for the private page and checks
a CSRF token on the authorization request. It currently allows any logged-in
demo user to request any `private-*` channel; it does not implement per-user
channel permissions. Without `CHANNEL_AUTH_SIGNING_KEY`, private authorization
is unavailable and Flask displays the Go service's error.

Private grants are valid for 30 seconds and are bound to one app, socket, and
channel. A disconnected socket cannot be authorized. A grant for another
socket or channel, or an expired grant, is rejected. The WebSocket sends a
`subscription_error` event for an unauthorized private subscribe. The demo UI
prints “subscribed” when it sends a command, not when Go confirms the Hub has
installed the subscription; that message alone is not proof of successful
subscription.

## Publish and receive events

The Flask page publishes to the channel currently entered in its form. Its
`POST /api/events` endpoint forwards this request to Go:

```json
{
  "event": "message",
  "topic": "notifications",
  "payload": {"text": "Hello!"}
}
```

For direct HTTP publishing, send the app secret as a bearer credential:

```sh
curl -X POST http://localhost:8080/apps/APP_ID/events \
  -H "Authorization: Bearer APP_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"event":"message","topic":"notifications","payload":{"text":"Hello!"}}'
```

Go returns `200` with `status: published` after accepting the event. A
successful response means the event was handed to the live delivery path; it
does not mean any subscriber received it. Connected subscribers in the same
app and exact channel receive a message with `channel`, `event`, and `data`
fields. Events are not stored for later delivery. Subscribers disconnected at
publish time miss the event, and reconnecting does not replay it.

Publishing to a `private-*` channel still requires the app secret, but does not
check which users are subscribed or whether they hold a grant. Private-channel
authorization controls subscribing; it is not an event-publishing permission
system.

## Common outcomes

| Situation | Result |
| --- | --- |
| Blank app name | Registration returns `400`. |
| Same app name registered again | Existing app ID, key, and secret are returned. |
| Invalid WebSocket app ID or key | Handshake returns `401`; the browser reports a connection error. |
| Missing `APP_ID`, `APP_KEY`, or `APP_SECRET` in Flask | The page shows the missing setting, or the publish/auth API returns `500`. |
| Missing channel or invalid event JSON | Go rejects the publish with `400`. Event, topic, and a valid JSON payload are required. |
| HTTP event request body larger than 1 MiB | Go returns `413 Payload Too Large`. Flask passes the upstream status through. |
| Missing or incorrect app secret on publish | Go returns `401`; Flask passes the upstream status through. |
| Go is unreachable from Flask | Flask returns `502` after its upstream request fails or times out. |
| Private auth requested while logged out | Flask returns `401`. |
| Missing or incorrect CSRF token on private auth | Flask returns `403`. |
| Private channel lacks the `private-` prefix | Flask rejects its auth request with `400`; direct Go auth requests also return `400`. |
| Private auth signing key is not configured | Go returns `503`; public channel usage remains available. |
| Private socket is disconnected, or grant is invalid/expired | Authorization or subscribe is rejected; unauthorized subscribe emits `subscription_error`. |
| WebSocket frame exceeds 64 KiB | Go closes that connection with WebSocket code `1009`. This is separate from the HTTP publish limit. |
| More than 20 WebSocket frames arrive within one second | Go closes that connection with code `1008` and reason `command rate limit exceeded`. |
| Client disconnects or network drops | Its subscriptions are removed; it must reconnect and subscribe again. There is no automatic retry or event replay in the example. |

## Scope of the demo

The Flask pages are examples, not a production authorization policy. The
private-channel flow demonstrates login, CSRF protection, and a Go-issued
grant, but does not associate channel permissions with individual users.
Delivery is live and best-effort: there is no durable event history, replay, or
exactly-once delivery guarantee.
