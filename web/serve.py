#!/usr/bin/env python3
"""Local preview server for the static QR pages.

GitHub Pages serves 404.html for any path that doesn't match a file, which is
what makes /qr/<payload> work. Python's stock http.server doesn't do that, so
this adds the same fallback.

    ./serve.py            # http://127.0.0.1:8000
    ./serve.py 9000       # pick a port
"""

import functools
import http.server
import os
import sys

DIRECTORY = os.path.dirname(os.path.abspath(__file__))


class Handler(http.server.SimpleHTTPRequestHandler):
    def send_head(self):
        if not os.path.exists(self.translate_path(self.path)):
            self.path = "/404.html"
        return super().send_head()


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8000
    handler = functools.partial(Handler, directory=DIRECTORY)
    server = http.server.HTTPServer(("127.0.0.1", port), handler)
    print(f"serving {DIRECTORY} at http://127.0.0.1:{port}")
    print(f"try http://127.0.0.1:{port}/qr/aGVsbG8gd29ybGQ")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
