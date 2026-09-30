# Go Pusher

A small **Pusher-style** service built with Go, WebSockets, and SQLite. The Go
service registers apps and routes events by app and channel. A Flask client
demonstrates publishing and subscribing with a registered app.

## Run Go

```sh
go run .
```

Open <http://localhost:8080> and register an app. Keep its credentials private.

App records are stored in `data/pusher.db` by default; set `PUSHER_DB_PATH` to
change the location. **Keep this database and its backups private.** The current
registry stores the app secret in the database as well as its hash, so treat the
database as sensitive. The `data/` directory is excluded from Git.

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

Open <http://localhost:5000> to **subscribe**, **listen**, and **publish**. The
`APP_SECRET` is used by Flask on the server and is not sent to the browser.

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
- `GET /apps/{app_id}/ws?key={app_key}` opens an app's WebSocket connection.

**Private-channel authorization is not implemented yet.**
