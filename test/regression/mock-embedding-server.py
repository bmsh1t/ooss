#!/usr/bin/env python3
import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def score_term(text, *terms):
    lowered = text.lower()
    for term in terms:
        if term in lowered:
            return 1.0
    return 0.0


def build_embedding(text):
    return [
        score_term(text, "sql", "union", "injection", "sqli"),
        score_term(text, "auth", "login", "token", "jwt", "session"),
        score_term(text, "admin", "panel", "preview", "console", "dashboard"),
        score_term(text, "xss", "payload", "script", "html", "reflected"),
    ]


class MockEmbeddingHandler(BaseHTTPRequestHandler):
    server_version = "mock-embeddings/1.0"

    def log_message(self, format, *args):
        return

    def _send_json(self, status, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self._send_json(200, {"ok": True})
            return
        self._send_json(404, {"error": {"message": "not found"}})

    def do_POST(self):
        if not self.path.endswith("/embeddings"):
            self._send_json(404, {"error": {"message": "not found"}})
            return

        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)
        try:
            payload = json.loads(raw.decode("utf-8") if raw else "{}")
        except json.JSONDecodeError:
            self._send_json(400, {"error": {"message": "invalid json"}})
            return

        inputs = payload.get("input", [])
        if isinstance(inputs, str):
            inputs = [inputs]
        if not isinstance(inputs, list):
            self._send_json(400, {"error": {"message": "input must be a string array"}})
            return

        model = payload.get("model") or self.server.model
        data = []
        for idx, text in enumerate(inputs):
            data.append(
                {
                    "object": "embedding",
                    "embedding": build_embedding(str(text)),
                    "index": idx,
                }
            )

        self._send_json(
            200,
            {
                "object": "list",
                "data": data,
                "model": model,
            },
        )


def main():
    parser = argparse.ArgumentParser(description="Local mock embeddings server for regression tests")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--model", default="test-embedding-3-small")
    args = parser.parse_args()

    server = ThreadingHTTPServer((args.host, args.port), MockEmbeddingHandler)
    server.model = args.model
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
