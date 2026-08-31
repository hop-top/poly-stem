<?php

declare(strict_types=1);

namespace HopTop\StemExample\CurrentTime;

use GuzzleHttp\Client as GuzzleClient;
use GuzzleHttp\Psr7\Response as GuzzleResponse;
use HopTop\Xrr\Adapters\HttpAdapter;
use HopTop\Xrr\Session as XrrSession;
use Psr\Http\Client\ClientInterface;
use Psr\Http\Message\RequestInterface;
use Psr\Http\Message\ResponseInterface;

/**
 * PSR-18 HTTP client that records each request through xrr.
 *
 * xrr's PHP HttpAdapter defines the wire-level fingerprint
 * (method+path+body hash) and cassette shape; this class is the
 * ~80-line glue between PSR-18 and that adapter. In replay mode `$do`
 * is never invoked; the adapter returns the recorded
 * `{status, headers, body}` which we marshal back into a Guzzle
 * Response so any PSR-18 consumer (openai-php/client included) can
 * decode it like any normal response.
 */
final class XrrHttpClient implements ClientInterface
{
    public function __construct(
        private readonly XrrSession $session,
        private readonly ClientInterface $inner,
    ) {
    }

    public function sendRequest(RequestInterface $req): ResponseInterface
    {
        $body = (string) $req->getBody();

        $xreq = [
            'method'  => $req->getMethod(),
            'url'     => (string) $req->getUri(),
            'headers' => $this->headersToFlat($req),
            'body'    => $body,
        ];

        $adapter = new HttpAdapter();
        $do = function () use ($req): array {
            $resp = $this->inner->sendRequest($req);
            $body = (string) $resp->getBody();
            // Re-seat body so the caller can re-read if needed.
            $resp->getBody()->rewind();
            $headers = [];
            foreach ($resp->getHeaders() as $name => $values) {
                $headers[$name] = $values[0] ?? '';
            }

            return [
                'status'  => $resp->getStatusCode(),
                'headers' => $headers,
                'body'    => $body,
            ];
        };

        /** @var array{status: int|string, headers: array<string, string>, body: string} $xresp */
        $xresp = $this->session->record($adapter, $xreq, $do);

        $status  = (int) ($xresp['status'] ?? 200);
        $headers = $xresp['headers'] ?? [];
        $body    = $xresp['body']    ?? '';

        if (!isset($headers['Content-Type']) && !isset($headers['content-type'])) {
            $headers['Content-Type'] = 'application/json';
        }

        return new GuzzleResponse($status, $headers, $body);
    }

    /** @return array<string, string> */
    private function headersToFlat(RequestInterface $req): array
    {
        $out = [];
        foreach ($req->getHeaders() as $name => $values) {
            $out[$name] = $values[0] ?? '';
        }

        return $out;
    }

    public static function guzzleInner(): ClientInterface
    {
        return new GuzzleClient(['http_errors' => false, 'timeout' => 30.0]);
    }
}
