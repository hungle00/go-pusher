## Go Pusher

A small Pusher-style service. App records persist in SQLite at
`data/pusher.db` by default. Set `PUSHER_DB_PATH` to use another file path.
Only a hash of each app secret is stored; the generated secret is returned once
when the app is created.

Run the service:

```sh
go run .
```

Keep the SQLite database file and its backups private. The default `data/`
directory is excluded from Git.

Install the Ruby client's dependency with `gem install httparty`, then create
an app and publish an event:

```ruby
require_relative "clients/pusher_client"

pusher = PusherClient.create_app("My App")
puts "App ID: #{pusher.app_id}"
puts "App key: #{pusher.app_key}"
pusher.trigger("notifications", "message", { text: "Hello from Ruby!" })
```

Open `http://localhost:8080`, enter the app ID and app key, connect, and
subscribe to `notifications` to receive published events.

`POST /apps` accepts `{ "name": "My App" }` and returns the app ID, public
key, and secret. Publish to `POST /apps/{app_id}/events` with a bearer secret
and `{ "event": "message", "topic": "notifications", "payload": { ... } }`.
Websocket clients connect to `/apps/{app_id}/ws?key={app_key}` and send
`{ "action": "subscribe", "channel": "notifications" }`.
