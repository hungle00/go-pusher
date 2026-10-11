import os
import secrets
import sqlite3
from typing import Any, Dict

import requests
from flask import Flask, jsonify, redirect, render_template, request, session, url_for
from werkzeug.security import check_password_hash, generate_password_hash

app = Flask(__name__, template_folder="templates")
app.secret_key = os.getenv("FLASK_SECRET_KEY", "dev-only-change-me")
GO_BASE_URL = os.getenv("GO_PUSHER_URL", "http://localhost:8080")
USER_DB_PATH = os.getenv("USER_DB_PATH", "data/flask_users.db")
APP_ID = os.getenv("APP_ID", "").strip()
APP_KEY = os.getenv("APP_KEY", "").strip()
APP_SECRET = os.getenv("APP_SECRET", "").strip()


def init_user_db() -> None:
    if USER_DB_PATH != ":memory:":
        os.makedirs(os.path.dirname(USER_DB_PATH) or ".", exist_ok=True)
    with sqlite3.connect(USER_DB_PATH) as connection:
        connection.execute(
            "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL)"
        )


init_user_db()


def trigger_event_on_go(app_id: str, secret: str, channel: str, event: str, payload: Any) -> Dict[str, Any]:
    response = requests.post(
        f"{GO_BASE_URL}/apps/{app_id}/events",
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {secret}",
        },
        json={
            "event": event,
            "topic": channel,
            "payload": payload,
        },
        timeout=5,
    )
    response.raise_for_status()
    return response.json()


def authorize_private_channel_on_go(socket_id: str, channel: str, channel_data: Dict[str, Any] | None = None) -> Dict[str, Any]:
    payload: Dict[str, Any] = {"socket_id": socket_id, "channel": channel}
    if channel_data is not None:
        payload["channel_data"] = channel_data
    response = requests.post(
        f"{GO_BASE_URL}/apps/{APP_ID}/private-channel-auth",
        headers={"Authorization": f"Bearer {APP_SECRET}"},
        json=payload,
        timeout=5,
    )
    response.raise_for_status()
    return response.json()


@app.route("/")
def index():
    missing_setting = next(
        (name for name, value in (("APP_ID", APP_ID), ("APP_KEY", APP_KEY), ("APP_SECRET", APP_SECRET)) if not value),
        None,
    )
    initial_status = f"Missing required Flask setting: {missing_setting}." if missing_setting else "Not connected."
    return render_template(
        "index.html",
        app_id=APP_ID,
        app_key=APP_KEY,
        go_base_url=GO_BASE_URL,
        initial_status=initial_status,
    )


@app.route("/login")
def login_page():
    next_path = request.args.get("next", "/private")
    if not next_path.startswith("/") or next_path.startswith("//"):
        next_path = "/private"
    return render_template("login.html", next_path=next_path)


@app.route("/private")
def private_page():
    if "username" not in session:
        return redirect(url_for("login_page", next="/private"))
    missing_setting = next(
        (name for name, value in (("APP_ID", APP_ID), ("APP_KEY", APP_KEY), ("APP_SECRET", APP_SECRET)) if not value),
        None,
    )
    initial_status = f"Missing required Flask setting: {missing_setting}." if missing_setting else "Not connected."
    return render_template(
        "private.html",
        app_id=APP_ID,
        app_key=APP_KEY,
        go_base_url=GO_BASE_URL,
        initial_status=initial_status,
        csrf_token=session.setdefault("csrf_token", secrets.token_urlsafe(32)),
        username=session["username"],
    )


@app.route("/api/events", methods=["POST"])
def publish_event():
    data = request.get_json(silent=True) or {}
    channel = (data.get("channel") or "").strip()
    event = (data.get("event") or "message").strip()
    payload = data.get("payload") or {}

    if not APP_ID:
        return jsonify({"error": "Missing required Flask setting: APP_ID."}), 500
    if not APP_SECRET:
        return jsonify({"error": "Missing required Flask setting: APP_SECRET."}), 500
    if not channel:
        return jsonify({"error": "channel is required."}), 400

    try:
        result = trigger_event_on_go(APP_ID, APP_SECRET, channel, event, payload)
    except requests.HTTPError as exc:
        return jsonify({"error": exc.response.text}), exc.response.status_code
    except requests.RequestException as exc:
        return jsonify({"error": str(exc)}), 502

    return jsonify(result), 200


@app.route("/api/register", methods=["POST"])
def register_user():
    data = request.get_json(silent=True) or {}
    username = (data.get("username") or "").strip()
    password = data.get("password") or ""
    if not username or len(password) < 8:
        return jsonify({"error": "username and a password of at least 8 characters are required."}), 400
    try:
        with sqlite3.connect(USER_DB_PATH) as connection:
            cursor = connection.execute(
                "INSERT INTO users (username, password_hash) VALUES (?, ?)",
                (username, generate_password_hash(password)),
            )
            user_id = cursor.lastrowid
    except sqlite3.IntegrityError:
        return jsonify({"error": "username already exists."}), 409
    session["username"] = username
    session["user_id"] = user_id
    return jsonify({"username": username}), 201


@app.route("/api/login", methods=["POST"])
def login_user():
    data = request.get_json(silent=True) or {}
    username = (data.get("username") or "").strip()
    password = data.get("password") or ""
    with sqlite3.connect(USER_DB_PATH) as connection:
        row = connection.execute("SELECT id, password_hash FROM users WHERE username = ?", (username,)).fetchone()
    if not row or not check_password_hash(row[1], password):
        return jsonify({"error": "invalid username or password."}), 401
    session["username"] = username
    session["user_id"] = row[0]
    return jsonify({"username": username}), 200


@app.route("/api/logout", methods=["POST"])
def logout_user():
    session.clear()
    return jsonify({"status": "logged out"}), 200


@app.route("/api/private-channel-auth", methods=["POST"])
def private_channel_auth():
    if "username" not in session:
        return jsonify({"error": "login is required."}), 401
    if request.headers.get("X-CSRF-Token") != session.get("csrf_token"):
        return jsonify({"error": "invalid CSRF token."}), 403
    if not APP_ID or not APP_SECRET:
        return jsonify({"error": "APP_ID and APP_SECRET are required in Flask settings."}), 500

    data = request.get_json(silent=True) or {}
    socket_id = (data.get("socket_id") or "").strip()
    channel = (data.get("channel") or "").strip()
    is_private = channel.startswith("private-")
    is_presence = channel.startswith("presence-")
    if not socket_id or (not is_private and not is_presence):
        return jsonify({"error": "socket_id and a private-* or presence-* channel are required."}), 400

    channel_data = None
    if is_presence:
        user_id = session.get("user_id")
        if user_id is None:
            return jsonify({"error": "logged-in user identity is missing."}), 401
        channel_data = {
            "user_id": str(user_id),
            "user_info": {"name": session["username"]},
        }

    try:
        result = authorize_private_channel_on_go(socket_id, channel, channel_data)
    except requests.HTTPError as exc:
        return jsonify({"error": exc.response.text}), exc.response.status_code
    except requests.RequestException as exc:
        return jsonify({"error": str(exc)}), 502
    return jsonify(result), 200


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000, debug=True)
