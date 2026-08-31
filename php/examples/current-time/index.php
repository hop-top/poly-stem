<?php

// current-time — smallest viable stem PHP agent loop.
//
// Mirrors go/examples/current-time/main.go: one tool-use round-trip
// against the OpenAI Chat Completions API, wired manually through a
// fresh stem Envelope.
//
//  1. Construct an xrr Session (default mode = replay; XRR_MODE=record
//     to capture a real request).
//  2. Build a PSR-18 client that routes through xrr (XrrHttpClient).
//  3. Hand that client to openai-php/client via
//     OpenAI::factory()->withHttpClient(...).
//  4. Build a fresh stem Envelope + user turn.
//  5. Call OpenAI with one `current_time` tool definition.
//  6. Project the assistant's tool_call into a stem ContentPart.
//  7. Execute the tool locally; append the tool-role turn.
//  8. Second OpenAI call with the tool result echoed back; append the
//     final assistant text.
//  9. Serialize to ./session.jsonl, reload, and validate.
//
// TODO(post-cassettes): verify replay end-to-end. The shared
// ./cassettes/ directory is being produced by a
// parallel Go agent against the Anthropic API; once an OpenAI-shaped
// cassette lands (separate fingerprint namespace because URL + body
// differ) this example will replay deterministically with no key.

declare(strict_types=1);

require __DIR__ . '/vendor/autoload.php';

use HopTop\Stem\Content\TextPart;
use HopTop\Stem\Content\ToolCallPart;
use HopTop\Stem\Content\ToolResultPart;
use HopTop\Stem\Envelope;
use HopTop\Stem\RoleEnum;
use HopTop\Stem\Turn;
use HopTop\StemExample\CurrentTime\XrrHttpClient;
use HopTop\Xrr\FileCassette;
use HopTop\Xrr\Mode;
use HopTop\Xrr\Session as XrrSession;
use OpenAI\Client as OpenAIClient;

use function HopTop\Stem\parseEnvelope;
use function HopTop\Stem\serializeEnvelope;
use function HopTop\Stem\validate;

const CASSETTE_DIR  = __DIR__ . '/cassettes';
const MODEL         = 'gpt-4o-mini';
const USER_PROMPT   = 'What time is it?';
const TOOL_NAME     = 'current_time';
const JSONL_PATH    = __DIR__ . '/session.jsonl';
const FIXED_TIME    = '2026-01-01T00:00:00Z';

try {
    main();
} catch (Throwable $e) {
    fwrite(STDERR, sprintf("current-time: %s\n", $e->getMessage()));
    exit(1);
}

function main(): void
{
    // --- 1. xrr session.
    if (!is_dir(CASSETTE_DIR)) {
        @mkdir(CASSETTE_DIR, 0o755, true);
    }
    $mode      = resolveMode(getenv('XRR_MODE') ?: 'replay');
    $xrrSess   = new XrrSession($mode, new FileCassette(CASSETTE_DIR));

    // --- 2 + 3. PSR-18 xrr client + OpenAI client.
    $apiKey = getenv('OPENAI_API_KEY') ?: 'replay-mode-no-key-required';
    $openai = OpenAI::factory()
        ->withApiKey($apiKey)
        ->withHttpClient(new XrrHttpClient($xrrSess, XrrHttpClient::guzzleInner()))
        ->make();

    // --- 4. fresh stem Envelope + user turn.
    $env = Envelope::fresh('current-time-demo');
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
            'description' => 'Returns the current UTC time as an ISO 8601 string.',
            'parameters'  => [
                'type'       => 'object',
                'properties' => new stdClass(),
                'required'   => [],
            ],
        ],
    ];

    $first = $openai->chat()->create([
        'model'    => MODEL,
        'messages' => [
            ['role' => 'user', 'content' => USER_PROMPT],
        ],
        'tools'    => [$toolDef],
    ]);

    // --- 6. project assistant turn → stem.
    [$assistantTurn, $toolCallId] = assistantTurnFromMessage($first, 't-assistant-1');
    $env = $env->appendTurn($assistantTurn);
    if ($toolCallId === '') {
        throw new RuntimeException('model returned no tool_call; cassette/model out of sync');
    }

    // --- 7. execute tool locally + append tool-role turn.
    $toolResult = executeCurrentTime();
    $env = $env->appendTurn(new Turn(
        id:        't-tool-2',
        role:      RoleEnum::Tool,
        createdAt: FIXED_TIME,
        content:   [new ToolResultPart($toolCallId, $toolResult, isError: false)],
    ));

    // --- 8. second call with assistant + tool messages echoed back.
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
    [$finalTurn, ] = assistantTurnFromMessage($second, 't-assistant-3');
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
 * Project an OpenAI Chat completion into a stem assistant Turn and
 * surface the first tool_call id (or empty string when no tool was called).
 *
 * @return array{0: Turn, 1: string}
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

    if (is_string($msg->content) && $msg->content !== '') {
        $parts[] = new TextPart($msg->content);
    }

    $toolCalls = $msg->toolCalls ?? [];
    foreach ($toolCalls as $call) {
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
    ];
}

/** Convert a returned chat message back into the array form needed for the next OpenAI call. */
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

/** Local handler: fixed clock so the cassette stays stable across re-records. */
function executeCurrentTime(): array
{
    return ['time' => FIXED_TIME, 'zone' => 'UTC'];
}
