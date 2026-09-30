# Add this to your Gemfile:
# source "https://rubygems.org"
# gem "httparty", "~> 0.23.2"

require "httparty"
require "json"

class PusherClient
  include HTTParty
  base_uri "http://localhost:8080"

  attr_reader :app_id, :app_key

  def self.create_app(name)
    response = post(
      "/apps",
      body: { name: name }.to_json,
      headers: { "Content-Type" => "application/json" }
    )

    raise "App creation failed: #{response.body}" unless response.success?

    payload = JSON.parse(response.body)
    new(payload["id"], payload["secret"], payload["key"])
  end

  def initialize(app_id, secret = nil, app_key = nil)
    @app_id = app_id
    @secret = secret
    @app_key = app_key
  end

  def events_url
    "/apps/#{@app_id}/events"
  end

  def trigger(channel, event, data)
    headers = {
      "Content-Type" => "application/json"
    }

    if @secret && !@secret.empty?
      headers["Authorization"] = "Bearer #{@secret}"
    end

    response = self.class.post(
      events_url,
      body: {
        event: event,
        topic: channel,
        payload: data
      }.to_json,
      headers: headers
    )

    logging(response)
    response
  end

  def logging(response)
    if response.success?
      puts "✅ Request successful!"
      puts JSON.parse(response.body)
    else
      puts "❌ Request failed!"
      puts JSON.parse(response.body)
    end
  rescue JSON::ParserError
    puts response.body
  end
end

# Example usage:
# pusher = PusherClient.create_app("My App")
# puts "App ID: #{pusher.app_id}"
# puts "App key: #{pusher.app_key}"
# pusher.trigger("notifications", "message", { text: "Hello from Ruby!" })

if __FILE__ == $PROGRAM_NAME
  pusher = PusherClient.create_app("my-app")
  puts "App ID: #{pusher.app_id}"
  puts "App key: #{pusher.app_key}"
  pusher.trigger("notifications", "message", { text: "Hello from Ruby!" })
end
