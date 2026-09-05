"""Minimal real AgentCore HTTP contract; no model, secrets, or shell endpoint."""
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

class Handler(BaseHTTPRequestHandler):
    def reply(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.reply(200 if self.path == "/ping" else 404, {"status": "Healthy"})

    def do_POST(self):
        size = int(self.headers.get("Content-Length", "0"))
        if self.path != "/invocations" or not 0 < size <= 4096:
            self.reply(400, {"error": "invalid request"})
            return
        try:
            payload = json.loads(self.rfile.read(size))
        except ValueError:
            self.reply(400, {"error": "invalid JSON"})
            return
        self.reply(200 if payload == {"operation": "health"} else 400,
                   {"status": "ready", "model_calls": 0})

    def log_message(self, fmt, *args):
        pass

HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
