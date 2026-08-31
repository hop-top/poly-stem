<?php

// web-search — second stem PHP agent example.
//
// Same envelope-wiring shape as ../current-time/, but the tool is
// `web_search`, executed by POST-ing to the Tavily search API. One
// stem Envelope captures the full transcript; two upstream services
// (OpenAI + Tavily) get recorded into the same xrr cassette directory
// because each HTTP call has a distinct method+URL+body fingerprint.
//
//  1. xrr Session.
//  2. PSR-18 xrr client wrapping Guzzle.
//  3. OpenAI client via factory()->withHttpClient(...).
//  4. Fresh stem Envelope + user turn.
//  5. First OpenAI call with web_search(query) tool definition.
//  6. Project assistant tool_call → stem.
//  7. Execute tool: POST to api.tavily.com (xrr-wrapped).
//  8. Second OpenAI call with the tool result echoed back.
//  9. Append final assistant text + write + reload + validate.
//
// TODO(post-cassettes): verify replay end-to-end. The shared cassette
// directory `./cassettes/` is being populated by
// a parallel Go-SDK agent against Anthropic. OpenAI cassettes are a
// separate fingerprint namespace.

declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use GuzzleHttp\Psr7\Request as PsrRequest;
use HopTop\Stem\Content\TextPart;
use HopTop\Stem\Content\ToolCallPart;
use HopTop\Stem\Content\ToolResultPart;
use HopTop\Stem\Envelope;
use HopTop\Stem\RoleEnum;
use HopTop\Stem\Turn;
use HopTop\StemExample\WebSearch\XrrHttpClient;
use HopTop\Xrr\FileCassette;
use HopTop\Xrr\Mode;
use HopTop\Xrr\Session as XrrSession;

use function HopTop\Stem\parseEnvelope;
use function HopTop\Stem\serializeEnvelope;
use function HopTop\Stem\validate;

const CASSETTE_DIR    = __DIR__ . '/cassettes';
const MODEL           = 'gpt-4o-mini';
const USER_PROMPT     = "What's the latest stable Go release?";
const TOOL_NAME       = 'web_search';
const TAVILY_URL      = 'https://api.tavily.com/search';
const MAX_RESULTS     = 3;
const JSONL_PATH      = __DIR__ . '/session.jsonl';
const FIXED_TIME      = '2026-01-01T00:00:00Z';

try {
    main();
} catch (Throwable $e) {
    fwrite(STDERR, sprintf("web-search: %s\n", $e->getMessage()));
    exit(1);
}

function main(): void
{
    // --- 1 + 2. xrr session + PSR-18 wrapper.
    if (!is_dir(CASSETTE_DIR)) {
        @mkdir(CASSETTE_DIR, 0o755, true);
    }
    $mode      = resolveMode(getenv('XRR_MODE') ?: 'replay');
    $xrrSess   = new XrrSession($mode, new FileCassette(CASSETTE_DIR));
    $httpClient = new XrrHttpClient($xrrSess, XrrHttpClient::guzzleInner());

    // --- 3. OpenAI client over the xrr-wrapped PSR-18 transport.
    $openaiKey = getenv('OPENAI_API_KEY') ?: 'replay-mode-no-key-required';
    $openai = OpenAI::factory()
        ->withApiKey($openaiKey)
        ->withHttpClient($httpClient)
        ->make();

    $tavilyKey = getenv('TAVILY_API_KEY') ?: 'replay-mode-no-key-required';

    // --- 4. fresh stem Envelope + user turn.
    $env = Envelope::fresh('web-search-demo');
    $env = $env->appendTurn(new Turn(
        id:        't-user-0',
        role:      RoleEnum::User,
        createdAt: FIXED_TIME,
        content:   [new TextPart(USER_PROMPT)],
    ));

    // --- 5. tool definition + first OpenAI call.
    $toolDef = [
        'type'     => 'function',
        'function' => [
            'name'        => TOOL_NAME,
            'description' => 'Run a web search and return the top results.',
            'parameters'  => [
                'type'       => 'object',
                'properties' => [
                    'query' => [
                        'type'        => 'string',
                        'description' => 'Search query string.',
                    ],
                ],
                'required'   => ['query'],
            ],
        ],
    ];

    $first = $openai->chat()->create([
        'model'    => MODEL,
        'messages' => [['role' => 'user', 'content' => USER_PROMPT]],
        'tools'    => [$toolDef],
    ]);

    // --- 6. project assistant turn.
    [$assistantTurn, $toolCallId, $toolArgs] = assistantTurnFromMessage($first, 't-assistant-1');
    $env = $env->appendTurn($assistantTurn);
    if ($toolCallId === '') {
        throw new RuntimeException('model returned no tool_call; cassette/model out of sync');
    }

    // --- 7. execute tool against Tavily over the xrr-wrapped client.
    $query = (string) ($toolArgs['query'] ?? USER_PROMPT);
    if ($query === '') {
        $query = USER_PROMPT;
    }
    $results    = callTavily($httpClient, $tavilyKey, $query);
    $toolResult = ['query' => $query, 'results' => $results];

    $env = $env->appendTurn(new Turn(
        id:        't-tool-2',
        role:      RoleEnum::Tool,
        createdAt: FIXED_TIME,
        content:   [new ToolResultPart($toolCallId, $toolResult, isError: false)],
    ));

    // --- 8. second OpenAI call with the tool result echoed back.
    $assistantMsg = openaiAssistantMessage($first);
    $second = $openai->chat()->create([
        'model'    => MODEL,
        'messages' => [
            ['role' => 'user', 'content' => USER_PROMPT],
            $assistantMsg,
            [
                'role'         => 'tool',
                'tool_call_id' => $toolCallId,
                'content'      => json_encode($toolResult, JSON_UNESCAPED_SLASHES),
            ],
        ],
        'tools'    => [$toolDef],
    ]);
    [$finalTurn, , ] = assistantTurnFromMessage($second, 't-assistant-3');
    $env = $env->appendTurn($finalTurn);

    // --- 9. write JSONL, reload, validate.
    file_put_contents(JSONL_PATH, serializeEnvelope($env) . "\n");
    fprintf(STDOUT, "wrote %s (%d turns)\n", JSONL_PATH, count($env->turns));

    $reloaded = parseEnvelope((string) file_get_contents(JSONL_PATH));
    $errors   = validate($reloaded);
    if ($errors !== []) {
        throw new RuntimeException('validate failed: ' . implode(' | ', array_map('strval', $errors)));
    }
    echo "envelope round-trip + validate: ok\n";
}

function resolveMode(string $mode): Mode
{
    return match ($mode) {
        'record'      => Mode::Record,
        'passthrough' => Mode::Passthrough,
        'replay', ''  => Mode::Replay,
        default       => throw new RuntimeException(sprintf('unknown XRR_MODE %s', $mode)),
    };
}

/**
 * Project an OpenAI Chat completion into a stem assistant Turn.
 *
 * @return array{0: Turn, 1: string, 2: array<string, mixed>}
 */
function assistantTurnFromMessage(\OpenAI\Responses\Chat\CreateResponse $resp, string $turnId): array
{
    $choice = $resp->choices[0] ?? null;
    if ($choice === null) {
        throw new RuntimeException('OpenAI returned no choices');
    }
    $msg = $choice->message;

    $parts     = [];
    $firstCall = '';
    /** @var array<string, mixed> $firstArgs */
    $firstArgs = [];

    if (is_string($msg->content) && $msg->content !== '') {
        $parts[] = new TextPart($msg->content);
    }

    foreach (($msg->toolCalls ?? []) as $call) {
        $rawArgs = $call->function->arguments ?? '{}';
        /** @var array<string, mixed> $args */
        $args = json_decode($rawArgs, true) ?: [];
        $parts[] = new ToolCallPart(
            callId: $call->id,
            name:   $call->function->name,
            input:  $args,
        );
        if ($firstCall === '') {
            $firstCall = $call->id;
            $firstArgs = $args;
        }
    }

    if ($parts === []) {
        throw new RuntimeException('assistant message had no usable content');
    }

    return [
        new Turn(
            id:        $turnId,
            role:      RoleEnum::Assistant,
            createdAt: FIXED_TIME,
            content:   $parts,
        ),
        $firstCall,
        $firstArgs,
    ];
}

function openaiAssistantMessage(\OpenAI\Responses\Chat\CreateResponse $resp): array
{
    $msg = $resp->choices[0]->message;
    $out = ['role' => 'assistant', 'content' => $msg->content ?? ''];
    if (!empty($msg->toolCalls)) {
        $out['tool_calls'] = array_map(static function ($call): array {
            return [
                'id'       => $call->id,
                'type'     => 'function',
                'function' => [
                    'name'      => $call->function->name,
                    'arguments' => $call->function->arguments,
                ],
            ];
        }, $msg->toolCalls);
    }

    return $out;
}

/**
 * POST the query to api.tavily.com via the xrr-wrapped PSR-18 client.
 * Normalises Tavily's response into a stable 3-tuple shape per result.
 *
 * @return list<array{title: string, url: string, snippet: string}>
 */
function callTavily(XrrHttpClient $client, string $apiKey, string $query): array
{
    $body = json_encode([
        'api_key'     => $apiKey,
        'query'       => $query,
        'max_results' => MAX_RESULTS,
    ], JSON_THROW_ON_ERROR);

    $req = new PsrRequest(
        'POST',
        TAVILY_URL,
        ['Content-Type' => 'application/json'],
        $body,
    );
    $resp = $client->sendRequest($req);
    $status = $resp->getStatusCode();
    $payload = (string) $resp->getBody();
    if ($status >= 300) {
        throw new RuntimeException(sprintf('tavily: status %d: %s', $status, $payload));
    }
    /** @var array{results?: list<array{title?: string, url?: string, content?: string}>} $decoded */
    $decoded = json_decode($payload, true) ?: [];
    $out = [];
    foreach (($decoded['results'] ?? []) as $r) {
        $out[] = [
            'title'   => (string) ($r['title']   ?? ''),
            'url'     => (string) ($r['url']     ?? ''),
            'snippet' => (string) ($r['content'] ?? ''),
        ];
    }

    return $out;
}
