## Go Pusher

A small Pusher-style service. App records persist in SQLite at
`data/pusher.db` by default. Set `PUSHER_DB_PATH` to use another file path.
Only a hash of each app secret is stored; the generated secret is returned once when the app is created.

Run the Go service:

```sh
go run .
```

Keep the SQLite database file and its backups private. The default `data/`
directory is excluded from Git.

### App registration flow
With the Go service running, open `http://localhost:8080` and register an app.
The Go page returns the app ID, public key, and secret. Keep the secret private;
it is stored as a hash and cannot be retrieved from SQLite later.

Example output:

```text
APP_ID=app_xxx
APP_KEY=key_xxx
APP_SECRET=secret_xxx
```

### Flask client
The Flask client lives under `clients/flask_app`. Its page uses the configured
app ID and key to subscribe to a channel, listen for messages, and publish events.
The app secret stays on the Flask server and is never sent to the browser.

In a second terminal, create a local environment file from the example and fill
in the credentials returned by Go:

```sh
cd clients/flask_app
cp .env.example .env
```

Edit `.env`, then install dependencies and start Flask:

```sh
python3 -m pip install flask requests
set -a
. ./.env
set +a
python3 app.py
```

Open `http://localhost:5000`, enter a channel, and connect to subscribe. The
same page can publish an event. Alternatively, publish over HTTP:

```bash
curl -X POST http://localhost:5000/api/events \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "notifications",
    "event": "message",
    "payload": {"text": "Hello from Flask!"}
  }'
```

The Go service exposes:

- `POST /apps` with `{ "name": "My App" }`
- `POST /apps/{app_id}/events` with bearer secret and JSON body:
  `{ "event": "message", "topic": "notifications", "payload": { ... } }`
- `GET /apps/{app_id}/ws?key={app_key}` for websocket subscriptions

This is closer to the real Pusher model: the app is created once by the server/admin layer, and clients reuse that registered app instead of creating arbitrary apps in the browser.
