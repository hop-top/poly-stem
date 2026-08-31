/**
 * xrrhttp.ts — adapter glue between fetch and the xrr HTTP recorder.
 *
 * One xrr session captures every outbound HTTP call (Anthropic + Tavily).
 * Each call is uniquely keyed by method + URL + sha256(body), so both
 * upstream services co-exist cleanly in the same cassette directory.
 *
 * Default mode = replay. Set XRR_MODE=record (+ ANTHROPIC_API_KEY and
 * TAVILY_API_KEY) to refresh cassettes from upstream.
 */

import {
  FileCassette,
  FileSession,
  HttpAdapter,
  type Mode,
  type HttpRequest,
  type HttpResponse,
} from "@hop-top/xrr/dist/src/index.js";

export type XrrMode = Mode;

export function modeFromEnv(env: string | undefined): XrrMode {
  switch (env) {
    case undefined:
    case "":
    case "replay":
      return "replay";
    case "record":
      return "record";
    case "passthrough":
      return "passthrough";
    default:
      throw new Error(`xrrhttp: unknown XRR_MODE "${env}"`);
  }
}

export function newXrrSession(cassetteDir: string, mode: XrrMode): FileSession {
  return new FileSession(mode, new FileCassette(cassetteDir));
}

export function makeXrrFetch(
  session: FileSession
): (input: RequestInfo | URL, init?: RequestInit) => Promise<Response> {
  const adapter = new HttpAdapter();

  return async (input, init) => {
    const req = new Request(input as RequestInfo, init);
    const method = req.method.toUpperCase();
    const url = req.url;
    const bodyText = method === "GET" || method === "HEAD" ? "" : await req.clone().text();

    const xreq: HttpRequest = {
      method,
      url,
      headers: headersToObject(req.headers),
      body: bodyText,
    };

    const xresp = await session.record(adapter, xreq, async () => {
      const realResp = await globalThis.fetch(req);
      const text = await realResp.text();
      const headers: Record<string, string> = {};
      realResp.headers.forEach((v, k) => {
        headers[k] = v;
      });
      const out: HttpResponse = { status: realResp.status, headers, body: text };
      return out;
    });

    return assembleResponse(xresp);
  };
}

function headersToObject(h: Headers): Record<string, string> {
  const out: Record<string, string> = {};
  h.forEach((v, k) => {
    out[k] = v;
  });
  return out;
}

function assembleResponse(xresp: HttpResponse): Response {
  const headers = new Headers();
  if (xresp.headers) {
    for (const [k, v] of Object.entries(xresp.headers)) headers.set(k, v);
  }
  if (!headers.has("content-type")) headers.set("content-type", "application/json");
  return new Response(xresp.body ?? "", {
    status: xresp.status,
    headers,
  });
}
