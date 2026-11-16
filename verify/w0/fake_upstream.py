#!/usr/bin/env python3
"""W0 冒烟用假上游：最小 OpenAI 兼容 /v1/chat/completions。

只用于本地转发验证，不联任何真实厂商。
"""
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _read_body(self):
        length = int(self.headers.get("Content-Length") or 0)
        return self.rfile.read(length) if length else b""

    def _json(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.rstrip("/") in ("/v1/models", "/models"):
            self._json(200, {"object": "list", "data": [{"id": "test-model", "object": "model"}]})
        else:
            self._json(404, {"error": {"message": "not found"}})

    def do_POST(self):
        raw = self._read_body()
        try:
            req = json.loads(raw or b"{}")
        except Exception:
            req = {}
        if self.path.rstrip("/") in ("/v1/chat/completions", "/chat/completions"):
            model = req.get("model", "unknown")
            print(f"[fake-upstream] chat model={model} bytes={len(raw)}", flush=True)
            self._json(200, {
                "id": "chatcmpl-w0",
                "object": "chat.completion",
                "created": 0,
                "model": model,
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": "pong from fake upstream"},
                    "finish_reason": "stop",
                }],
                "usage": {"prompt_tokens": 3, "completion_tokens": 4, "total_tokens": 7},
            })
        else:
            self._json(404, {"error": {"message": "not found"}})

    def log_message(self, fmt, *args):  # 静默访问日志，避免噪声
        pass


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 6801
    print(f"[fake-upstream] listening on 127.0.0.1:{port}", flush=True)
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
