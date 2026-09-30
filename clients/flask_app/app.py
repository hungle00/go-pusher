import os
from typing import Any, Dict

import requests
from flask import Flask, jsonify, render_template, request

app = Flask(__name__, template_folder="templates")
GO_BASE_URL = os.getenv("GO_PUSHER_URL", "http://localhost:8080")
APP_ID = os.getenv("APP_ID", "").strip()
APP_KEY = os.getenv("APP_KEY", "").strip()
APP_SECRET = os.getenv("APP_SECRET", "").strip()


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


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000, debug=True)
