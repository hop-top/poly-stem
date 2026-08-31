"""xrr_httpx — httpx transport that routes every request through xrr.

xrr's HTTP adapter (``xrr.adapters.http``) defines the on-disk cassette
format but does NOT ship a ``httpx`` transport. This ~80 LOC module
fills that gap so the Anthropic Python SDK can be pointed at an
xrr-wrapped HTTP transport via ``http_client=httpx.Client(transport=...)``.

In replay mode the inner transport is never invoked; xrr serves the
recorded ``HttpResponse`` whose body+headers we reassemble into an
``httpx.Response`` the SDK can decode.
"""
from __future__ import annotations

import os

import httpx
from xrr import RECORD, REPLAY, FileCassette, Session
from xrr.adapters.http import HttpAdapter, HttpRequest, HttpResponse


class XrrTransport(httpx.BaseTransport):
    """httpx transport that records or replays every request via xrr."""

    def __init__(self, session: Session, inner: httpx.BaseTransport | None = None) -> None:
        self._session = session
        self._inner = inner or httpx.HTTPTransport()
        self._adapter = HttpAdapter()

    def handle_request(self, request: httpx.Request) -> httpx.Response:
        body = request.content.decode("utf-8") if request.content else ""
        xreq = HttpRequest(
            method=request.method,
            url=str(request.url),
            headers={},  # headers excluded from fingerprint, mirroring Go
            body=body,
        )

        def do() -> HttpResponse:
            # Replay mode: do() is never called by xrr.Session.record.
            inner_resp = self._inner.handle_request(request)
            inner_body = b"".join(inner_resp.stream)
            inner_resp.close()
            headers = {k: v for k, v in inner_resp.headers.items()}
            return HttpResponse(
                status=inner_resp.status_code,
                headers=headers,
                body=inner_body.decode("utf-8", errors="replace"),
            )

        resp: HttpResponse = self._session.record(self._adapter, xreq, do)

        body_bytes = resp.body.encode("utf-8")
        headers = dict(resp.headers)
        headers.setdefault("Content-Type", "application/json")
        return httpx.Response(
            status_code=resp.status,
            headers=headers,
            content=body_bytes,
            request=request,
        )


def load_session(cassette_dir: str, mode_env: str | None) -> Session:
    """Resolve XRR_MODE from env (default: replay) and build a Session.

    Default-replay matches the example contract: a fresh checkout with
    no env vars set MUST run end-to-end against the vendored cassettes,
    no network, no API keys.
    """
    raw = (mode_env or "replay").lower()
    if raw in ("", "replay"):
        mode = REPLAY
    elif raw == "record":
        mode = RECORD
    else:
        raise ValueError(f"xrr_httpx: unknown XRR_MODE {raw!r}")
    os.makedirs(cassette_dir, exist_ok=True)
    return Session(mode, FileCassette(cassette_dir))


def new_http_client(cassette_dir: str) -> httpx.Client:
    """Convenience: build the httpx.Client the Anthropic SDK will use."""
    session = load_session(cassette_dir, os.environ.get("XRR_MODE"))
    transport = XrrTransport(session)
    return httpx.Client(transport=transport, timeout=httpx.Timeout(60.0))
