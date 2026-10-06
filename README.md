# Go Pusher

This project is a small vibe-code experiment: a lightweight **Pusher-style**
service built with Go, WebSockets, and SQLite. The Go service registers apps and
routes events by app and channel. A Flask client demonstrates publishing and
subscribing with a registered app.

## Run Go

```sh
export CHANNEL_AUTH_SIGNING_KEY="replace-with-a-long-random-signing-key"
# Optional; defaults to 8080.
export PORT=8080
go run .
```

Open <http://localhost:8080> and register an app. Keep its credentials private.
The Go page remembers the most recently registered app name and ID in this
browser and shows that app's currently active channels and subscriber counts.
Only the name and ID are remembered; app credentials are not saved in browser
storage. Channel counts are live hub state and reset when the Go process restarts.

App records are stored in `data/pusher.db` by default; set `PUSHER_DB_PATH` to
change the location. **Keep this database and its backups private.** 

## Run with Docker

Build and run the Go service with a persistent volume for its SQLite database:

```sh
docker build -t go-pusher .
docker run --rm -p 8080:8080 -v go-pusher-data:/data go-pusher
```

Open <http://localhost:8080>. The Flask client is not included in the image.

## Run Flask Client

In a second terminal, set up the client's environment:

```sh
cd clients/flask_app
cp .env.example .env
```

Put the credentials from the Go registration page in `.env`. Then install and
run Flask:

```sh
python3 -m pip install flask requests
set -a
. ./.env
set +a
python3 app.py
```

Open <http://localhost:5000> for **public channels**. Use the **Go to private
channel** link to reach the login page, then Flask redirects authenticated users
to the protected private-channel page. Flask stores users in its local SQLite
database, checks the session, and asks Go for a short-lived authorization. The
`APP_SECRET` is used server-to-server and is not sent to the browser.

To publish with HTTP instead of the page:

```sh
curl -X POST http://localhost:5000/api/events \
  -H "Content-Type: application/json" \
  -d '{"channel":"notifications","event":"message","payload":{"text":"Hello!"}}'
```

## API

- `POST /apps` registers an app: `{ "name": "My App" }`.
- `POST /apps/{app_id}/events` publishes an event. Flask sends the app secret
  as a bearer credential; the JSON body contains `event`, `topic`, and `payload`.
- `GET /apps/{app_id}/channels` returns the currently active channels and
  subscriber counts for that app.
- `GET /apps/{app_id}/ws?key={app_key}` opens an app's WebSocket connection.
- `POST /apps/{app_id}/private-channel-auth` issues a short-lived grant after
  Flask proves it knows the app secret and provides an active socket ID.

See [docs/private-channel-auth.md](docs/private-channel-auth.md) for the private
channel flow and implementation notes. For app credentials, Flask usage,
publishing, delivery behavior, and common errors, see
[docs/app-and-event-behavior.md](docs/app-and-event-behavior.md).
