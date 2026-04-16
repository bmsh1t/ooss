#!/usr/bin/env python3
import argparse
import json
import socketserver
import sys
import time
from http.server import BaseHTTPRequestHandler
from pathlib import Path


def flatten_input_text(value):
    parts = []
    if isinstance(value, str):
        parts.append(value)
    elif isinstance(value, list):
        for item in value:
            if isinstance(item, dict):
                content = item.get("content")
                if isinstance(content, list):
                    for part in content:
                        if isinstance(part, dict):
                            text = part.get("text")
                            if text:
                                parts.append(str(text))
                elif content is not None:
                    parts.append(str(content))
            elif item is not None:
                parts.append(str(item))
    elif value is not None:
        parts.append(str(value))
    return " ".join(parts)


def choose_response_text(payload):
    text = flatten_input_text(payload.get("input"))
    if "Search recent reconnaissance guidance" in text:
        return "Native workflow response"
    if "Summarize recon priorities" in text:
        return "Alias workflow response"
    if text == "hello responses":
        return "Function env response"
    if text == "latest recon news":
        return "Function custom response"
    if "Analyze example.com" in text:
        return "Function conversations response"
    return "Generic responses output"


class ReusableTCPServer(socketserver.TCPServer):
    allow_reuse_address = True


class Handler(BaseHTTPRequestHandler):
    request_log_path: Path | None = None

    def _write_json(self, status, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _log_request(self, body):
        if self.request_log_path is None:
            return
        record = {
            "method": self.command,
            "path": self.path,
            "body": body,
            "time": int(time.time()),
        }
        with self.request_log_path.open("a", encoding="utf-8") as f:
            f.write(json.dumps(record, ensure_ascii=False) + "\n")

    def do_GET(self):
        if self.path == "/health":
            self._write_json(200, {"ok": True})
            return
        self._write_json(404, {"error": "not found"})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length > 0 else b"{}"
        try:
            payload = json.loads(raw.decode("utf-8") or "{}")
        except json.JSONDecodeError:
            self._write_json(400, {"error": {"message": "invalid json"}})
            return

        self._log_request(payload)

        if self.path != "/v1/responses":
            self._write_json(404, {"error": {"message": f"unexpected path: {self.path}"}})
            return

        text = choose_response_text(payload)
        self._write_json(
            200,
            {
                "id": "resp-smoke-1",
                "object": "response",
                "created_at": int(time.time()),
                "status": "completed",
                "model": payload.get("model", "gpt-5.4"),
                "output": [
                    {
                        "id": "msg_1",
                        "type": "message",
                        "status": "completed",
                        "role": "assistant",
                        "content": [{"type": "output_text", "text": text}],
                    }
                ],
                "output_text": text,
                "usage": {"input_tokens": 10, "output_tokens": 4, "total_tokens": 14},
            },
        )

    def log_message(self, fmt, *args):
        sys.stderr.write("%s - - [%s] %s\n" % (self.client_address[0], self.log_date_time_string(), fmt % args))


def main():
    parser = argparse.ArgumentParser(description="Mock Responses API server for Osmedeus smoke tests")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--request-log", required=True)
    args = parser.parse_args()

    Handler.request_log_path = Path(args.request_log)
    Handler.request_log_path.parent.mkdir(parents=True, exist_ok=True)

    with ReusableTCPServer((args.host, args.port), Handler) as httpd:
        httpd.serve_forever()


if __name__ == "__main__":
    main()
