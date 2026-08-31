// xrrhttp.go — adapter glue between net/http and the xrr HTTP recorder.
//
// Identical shape to the current-time example: xrr's HTTP adapter
// (hop.top/xrr/adapters/http) defines the on-disk cassette format but
// does not ship a *http.Client wrapper. ~80 lines of glue fill that
// gap so the Anthropic SDK can be pointed at an xrr-wrapped HTTP
// transport via option.WithHTTPClient, and so the same approach
// transparently records the Tavily call below.
//
// In replay mode do() is never invoked; xrr serves a *xrr.RawResponse
// whose Payload map carries the recorded {status, headers, body}.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	xrr "hop.top/xrr"
	xhttp "hop.top/xrr/adapters/http"
)

// xrrClient implements option.HTTPClient (the Do(*http.Request) interface
// shape) and routes every request through an xrr session.
type xrrClient struct {
	session *xrr.FileSession
	inner   *http.Client
}

func newXRRClient(session *xrr.FileSession, inner *http.Client) *xrrClient {
	if inner == nil {
		inner = http.DefaultClient
	}
	return &xrrClient{session: session, inner: inner}
}

// Do reads req.Body so the xrr fingerprint hashes the real bytes, then
// hands the request to either the real transport (record / passthrough)
// or to xrr's replay path.
func (c *xrrClient) Do(req *http.Request) (*http.Response, error) {
	bodyBytes, err := drainBody(req)
	if err != nil {
		return nil, fmt.Errorf("xrrhttp: drain body: %w", err)
	}

	xreq := &xhttp.Request{
		Method: req.Method,
		URL:    req.URL.String(),
		Body:   string(bodyBytes),
	}

	adapter := xhttp.NewAdapter()
	do := func() (xrr.Response, error) {
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))

		resp, err := c.inner.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("xrrhttp: read response: %w", err)
		}
		headers := map[string]string{}
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		return &xhttp.Response{
			Status:  resp.StatusCode,
			Headers: headers,
			Body:    string(respBody),
		}, nil
	}

	resp, err := c.session.Record(req.Context(), adapter, xreq, do)
	if err != nil {
		return nil, err
	}

	return assembleResponse(req, resp)
}

func drainBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

// assembleResponse converts xrr's response shape (record or replay)
// back into a *http.Response the upstream SDK can decode.
func assembleResponse(req *http.Request, resp xrr.Response) (*http.Response, error) {
	var (
		status  int
		headers map[string]string
		body    string
	)
	switch r := resp.(type) {
	case *xhttp.Response:
		status, headers, body = r.Status, r.Headers, r.Body
	case *xrr.RawResponse:
		if s, ok := r.Payload["status"]; ok {
			switch v := s.(type) {
			case int:
				status = v
			case int64:
				status = int(v)
			case float64:
				status = int(v)
			case string:
				n, _ := strconv.Atoi(v)
				status = n
			}
		}
		if h, ok := r.Payload["headers"].(map[string]any); ok {
			headers = make(map[string]string, len(h))
			for k, v := range h {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}
		if b, ok := r.Payload["body"].(string); ok {
			body = b
		}
	default:
		return nil, fmt.Errorf("xrrhttp: unexpected xrr response type %T", resp)
	}

	httpResp := &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{},
		Body:          io.NopCloser(bytes.NewReader([]byte(body))),
		ContentLength: int64(len(body)),
		Request:       req,
	}
	for k, v := range headers {
		httpResp.Header.Set(k, v)
	}
	if _, ok := headers["Content-Type"]; !ok {
		httpResp.Header.Set("Content-Type", "application/json")
	}
	return httpResp, nil
}

// loadSession resolves the xrr mode from XRR_MODE (default: replay)
// and constructs a *FileSession pointed at the given cassette dir.
//
// Default-replay matches the example contract: a fresh checkout with
// no env vars set MUST run end-to-end against the vendored cassettes
// with no network and no API keys.
func loadSession(_ context.Context, cassetteDir, mode string) (*xrr.FileSession, error) {
	m := xrr.ModeReplay
	switch mode {
	case "record":
		m = xrr.ModeRecord
	case "passthrough":
		m = xrr.ModePassthrough
	case "", "replay":
		m = xrr.ModeReplay
	default:
		return nil, fmt.Errorf("xrrhttp: unknown XRR_MODE %q", mode)
	}
	return xrr.NewSession(m, xrr.NewFileCassette(cassetteDir)), nil
}
