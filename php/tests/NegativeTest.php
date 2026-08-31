<?php

declare(strict_types=1);

namespace HopTop\Stem\Tests;

use HopTop\Stem\Content\TextPart;
use HopTop\Stem\Content\ToolResultPart;
use HopTop\Stem\Envelope;
use HopTop\Stem\EnvelopeParseException;
use HopTop\Stem\RoleEnum;
use HopTop\Stem\Source;
use HopTop\Stem\Stem;
use HopTop\Stem\Turn;
use PHPUnit\Framework\TestCase;

use function HopTop\Stem\parseEnvelope;
use function HopTop\Stem\validate;
use function HopTop\Stem\validateBytes;

final class NegativeTest extends TestCase
{
    public function testRejectsBadCrtxVersion(): void
    {
        $env = new Envelope(
            crtxVersion: '0.2',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [],
        );
        $errors = validate($env);
        $this->assertNotEmpty($errors);
        $this->assertStringContainsString('unsupported', (string) $errors[0]);
    }

    public function testRejectsMalformedJson(): void
    {
        $this->expectException(EnvelopeParseException::class);
        parseEnvelope('not json');
    }

    public function testRejectsUnknownTopLevelField(): void
    {
        $bad = json_encode([
            'crtx_version' => '0.1',
            'id'           => 'x',
            'created_at'   => '2026-01-01T00:00:00Z',
            'updated_at'   => '2026-01-01T00:00:00Z',
            'source'       => ['kind' => 'stem', 'version' => '0.1.0'],
            'turns'        => [],
            'unknown_top'  => true,
        ]);
        $this->assertIsString($bad);
        $this->expectException(EnvelopeParseException::class);
        $this->expectExceptionMessageMatches('/unknown envelope field unknown_top/');
        parseEnvelope($bad);
    }

    public function testRejectsUnknownRole(): void
    {
        $bad = json_encode([
            'crtx_version' => '0.1',
            'id'           => 'x',
            'created_at'   => '2026-01-01T00:00:00Z',
            'updated_at'   => '2026-01-01T00:00:00Z',
            'source'       => ['kind' => 'stem', 'version' => '0.1.0'],
            'turns'        => [[
                'id'         => 't0',
                'role'       => 'overlord',
                'created_at' => '2026-01-01T00:00:00Z',
                'content'    => [['type' => 'text', 'text' => 'hi']],
            ]],
        ]);
        $this->assertIsString($bad);
        $this->expectException(EnvelopeParseException::class);
        $this->expectExceptionMessageMatches('/unknown role overlord/');
        parseEnvelope($bad);
    }

    public function testRejectsMissingRequiredField(): void
    {
        $bad = json_encode([
            'crtx_version' => '0.1',
            // 'id' missing
            'created_at'   => '2026-01-01T00:00:00Z',
            'updated_at'   => '2026-01-01T00:00:00Z',
            'source'       => ['kind' => 'stem', 'version' => '0.1.0'],
            'turns'        => [],
        ]);
        $this->assertIsString($bad);
        $this->expectException(EnvelopeParseException::class);
        $this->expectExceptionMessageMatches('/missing required field id/');
        parseEnvelope($bad);
    }

    public function testRejectsImageWithBothDataAndUrl(): void
    {
        $bad = json_encode([
            'crtx_version' => '0.1',
            'id'           => 'x',
            'created_at'   => '2026-01-01T00:00:00Z',
            'updated_at'   => '2026-01-01T00:00:00Z',
            'source'       => ['kind' => 'stem', 'version' => '0.1.0'],
            'turns'        => [[
                'id'         => 't0',
                'role'       => 'user',
                'created_at' => '2026-01-01T00:00:00Z',
                'content'    => [[
                    'type' => 'image',
                    'mime' => 'image/png',
                    'data' => 'abc',
                    'url'  => 'https://x/',
                ]],
            ]],
        ]);
        $this->assertIsString($bad);
        $this->expectException(EnvelopeParseException::class);
        parseEnvelope($bad);
    }

    public function testRejectsImageWithNeitherDataNorUrl(): void
    {
        $bad = json_encode([
            'crtx_version' => '0.1',
            'id'           => 'x',
            'created_at'   => '2026-01-01T00:00:00Z',
            'updated_at'   => '2026-01-01T00:00:00Z',
            'source'       => ['kind' => 'stem', 'version' => '0.1.0'],
            'turns'        => [[
                'id'         => 't0',
                'role'       => 'user',
                'created_at' => '2026-01-01T00:00:00Z',
                'content'    => [[
                    'type' => 'image',
                    'mime' => 'image/png',
                ]],
            ]],
        ]);
        $this->assertIsString($bad);
        $this->expectException(EnvelopeParseException::class);
        parseEnvelope($bad);
    }

    public function testRejectsMalformedExtensionType(): void
    {
        $bad = json_encode([
            'crtx_version' => '0.1',
            'id'           => 'x',
            'created_at'   => '2026-01-01T00:00:00Z',
            'updated_at'   => '2026-01-01T00:00:00Z',
            'source'       => ['kind' => 'stem', 'version' => '0.1.0'],
            'turns'        => [[
                'id'         => 't0',
                'role'       => 'assistant',
                'created_at' => '2026-01-01T00:00:00Z',
                'content'    => [['type' => 'x-bad space']],
            ]],
        ]);
        $this->assertIsString($bad);
        $this->expectException(EnvelopeParseException::class);
        $this->expectExceptionMessageMatches('/extension type/');
        parseEnvelope($bad);
    }

    public function testValidateFlagsOrphanedToolResult(): void
    {
        // Build an Envelope via the value constructor — bypasses parse
        // strictness so we can exercise the relational check directly.
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 't0',
                    role: RoleEnum::Tool,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolResultPart('orphan-call', ['ok' => true])],
                ),
            ],
        );
        $errors = validate($env);
        $this->assertNotEmpty($errors);
        $msgs = array_map(static fn ($e): string => (string) $e, $errors);
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'no preceding open tool_call')),
            'expected orphaned tool_result diagnostic, got: ' . implode(' | ', $msgs),
        );
    }

    public function testValidateBytesIsStricterThanValidate(): void
    {
        // Unknown top-level field — `validate()` cannot see this (the
        // value type discards unknown fields at decode time), but
        // `validateBytes()` must reject via the strict parse.
        $bad = json_encode([
            'crtx_version' => '0.1',
            'id'           => 'x',
            'created_at'   => '2026-01-01T00:00:00Z',
            'updated_at'   => '2026-01-01T00:00:00Z',
            'source'       => ['kind' => 'stem', 'version' => '0.1.0'],
            'turns'        => [],
            'phantom'      => 'value',
        ]);
        $this->assertIsString($bad);
        $this->expectException(EnvelopeParseException::class);
        validateBytes($bad);
    }

    public function testRejectsParentIdWithoutForkPoint(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [],
            parentId: 'parent-1',
            forkPoint: null,
        );
        $errors = validate($env);
        $this->assertNotEmpty($errors);
        $msgs = array_map(static fn ($e): string => (string) $e, $errors);
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'parent_id and fork_point')),
        );
    }

    public function testAcceptsEmptyThinkingText(): void
    {
        // thinking.text "" is valid per schema; only the key is required.
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 't0',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new \HopTop\Stem\Content\ThinkingPart('')],
                ),
            ],
        );
        $this->assertSame([], validate($env));
    }

    public function testRejectsBadImageMime(): void
    {
        // image.mime must match ^[a-z]+/[a-zA-Z0-9.+-]+$ per crtx v0.1.
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 't0',
                    role: RoleEnum::User,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new \HopTop\Stem\Content\ImagePart('NOT_A_MIME', 'aGk=')],
                ),
            ],
        );
        $errors = validate($env);
        $msgs   = array_map(static fn ($e): string => (string) $e, $errors);
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'mime')),
            'expected mime diagnostic, got: ' . implode(' | ', $msgs),
        );
    }

    public function testAcceptsEmptyToolCallInput(): void
    {
        // tool_call.input: {} is valid — tools may take no args. Raw
        // JSON used so the input literal is {} (not []).
        $raw = <<<'JSON'
        {
          "crtx_version": "0.1",
          "id": "x",
          "created_at": "2026-01-01T00:00:00Z",
          "updated_at": "2026-01-01T00:00:00Z",
          "source": {"kind": "stem", "version": "0.1.0"},
          "turns": [
            {"id": "a", "role": "assistant", "created_at": "2026-01-01T00:00:00Z",
             "content": [{"type": "tool_call", "call_id": "c1", "name": "current_time", "input": {}}]},
            {"id": "t", "role": "tool", "created_at": "2026-01-01T00:00:00Z",
             "content": [{"type": "tool_result", "call_id": "c1", "output": {"now": "2026"}}]}
          ]
        }
        JSON;
        $env    = parseEnvelope($raw);
        $errors = validate($env);
        $this->assertSame([], $errors);
    }

    public function testRejectsListShapedMetadata(): void
    {
        // metadata is typed `object` per crtx v0.1; a numeric-indexed
        // list ([1,2,3]) passes is_array() but is not an object.
        $bad = <<<'JSON'
        {
          "crtx_version": "0.1",
          "id": "x",
          "created_at": "2026-01-01T00:00:00Z",
          "updated_at": "2026-01-01T00:00:00Z",
          "source": {"kind": "stem", "version": "0.1.0"},
          "turns": [],
          "metadata": [1, 2, 3]
        }
        JSON;
        $this->expectException(EnvelopeParseException::class);
        $this->expectExceptionMessageMatches('/metadata must be an object/');
        parseEnvelope($bad);
    }

    public function testRejectsListShapedTurnMetadata(): void
    {
        $bad = <<<'JSON'
        {
          "crtx_version": "0.1",
          "id": "x",
          "created_at": "2026-01-01T00:00:00Z",
          "updated_at": "2026-01-01T00:00:00Z",
          "source": {"kind": "stem", "version": "0.1.0"},
          "turns": [{
            "id": "t0",
            "role": "user",
            "created_at": "2026-01-01T00:00:00Z",
            "content": [{"type": "text", "text": "hi"}],
            "metadata": ["a", "b"]
          }]
        }
        JSON;
        $this->expectException(EnvelopeParseException::class);
        $this->expectExceptionMessageMatches('/metadata must be an object/');
        parseEnvelope($bad);
    }

    public function testRejectsListShapedContentPartMetadata(): void
    {
        $bad = <<<'JSON'
        {
          "crtx_version": "0.1",
          "id": "x",
          "created_at": "2026-01-01T00:00:00Z",
          "updated_at": "2026-01-01T00:00:00Z",
          "source": {"kind": "stem", "version": "0.1.0"},
          "turns": [{
            "id": "t0",
            "role": "user",
            "created_at": "2026-01-01T00:00:00Z",
            "content": [{"type": "text", "text": "hi", "metadata": [1, 2]}]
          }]
        }
        JSON;
        $this->expectException(EnvelopeParseException::class);
        $this->expectExceptionMessageMatches('/metadata must be an object/');
        parseEnvelope($bad);
    }

    public function testRejectsEmptyContentAtParse(): void
    {
        // crtx v0.1 mandates content minItems=1. Previously only
        // validate() caught this; parseEnvelope now rejects directly.
        $bad = <<<'JSON'
        {
          "crtx_version": "0.1",
          "id": "x",
          "created_at": "2026-01-01T00:00:00Z",
          "updated_at": "2026-01-01T00:00:00Z",
          "source": {"kind": "stem", "version": "0.1.0"},
          "turns": [{
            "id": "t0",
            "role": "user",
            "created_at": "2026-01-01T00:00:00Z",
            "content": []
          }]
        }
        JSON;
        $this->expectException(EnvelopeParseException::class);
        $this->expectExceptionMessageMatches('/content must have/');
        parseEnvelope($bad);
    }

    public function testRejectsNegativeForkPointAtParse(): void
    {
        $bad = <<<'JSON'
        {
          "crtx_version": "0.1",
          "id": "x",
          "created_at": "2026-01-01T00:00:00Z",
          "updated_at": "2026-01-01T00:00:00Z",
          "source": {"kind": "stem", "version": "0.1.0"},
          "turns": [],
          "parent_id": "p",
          "fork_point": -1
        }
        JSON;
        $this->expectException(EnvelopeParseException::class);
        $this->expectExceptionMessageMatches('/fork_point must be >= 0/');
        parseEnvelope($bad);
    }

    public function testValidateAcceptsCorrectStemConstantsRoundTrip(): void
    {
        $env = Envelope::fresh('round-1');
        $env = $env->appendTurn(new Turn(
            id: 't0',
            role: RoleEnum::User,
            createdAt: '2026-01-01T00:00:00Z',
            content: [new TextPart('hello')],
        ));
        $this->assertSame(Stem::CRTX_VERSION, $env->crtxVersion);
        $this->assertSame([], validate($env));
    }
}
