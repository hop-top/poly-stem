<?php

declare(strict_types=1);

namespace HopTop\Stem\Tests;

use HopTop\Stem\Content\TextPart;
use HopTop\Stem\Content\ToolCallPart;
use HopTop\Stem\Content\ToolResultPart;
use HopTop\Stem\DispatchedFrom;
use HopTop\Stem\Envelope;
use HopTop\Stem\InjectedTurnRef;
use HopTop\Stem\RoleEnum;
use HopTop\Stem\Source;
use HopTop\Stem\Turn;
use PHPUnit\Framework\TestCase;

use function HopTop\Stem\parseEnvelope;
use function HopTop\Stem\serializeEnvelope;
use function HopTop\Stem\validate;

/**
 * crtx v0.1 multi-agent provenance tests. Mirrors the
 * "Multi-agent provenance" section of go/validate_test.go.
 *
 * Spec: envelope.md §3.1, §3.2, §6.2, §6.3, §7.2, §7.3.
 */
final class MultiAgentProvenanceTest extends TestCase
{
    // ---- Fixture round-trip ------------------------------------------------

    public function testParseDispatchFixtureValidates(): void
    {
        $json = $this->loadFixture('dispatch.json');
        $env  = parseEnvelope($json);
        $this->assertSame([], validate($env));
    }

    public function testParseDispatchNestedFixtureValidates(): void
    {
        $json = $this->loadFixture('dispatch-nested.json');
        $env  = parseEnvelope($json);
        $this->assertNotNull($env->dispatchedFrom);
        $this->assertSame(
            '01JCRTX0DISPATCHNESTED00PARENT',
            $env->dispatchedFrom->envelopeId,
        );
        $this->assertSame('call_C_dispatch_A', $env->dispatchedFrom->callId);
        $this->assertSame([], validate($env));
    }

    public function testParseInjectionFixtureValidates(): void
    {
        $json = $this->loadFixture('injection.json');
        $env  = parseEnvelope($json);
        $this->assertCount(2, $env->injectedTurns);
        $this->assertSame('01JCRTX0LOCALTURN0002', $env->injectedTurns[1]->injectedAfterTurnId);
        $this->assertSame([], validate($env));
    }

    public function testRoundTripMultiAgentFixtures(): void
    {
        foreach (['dispatch.json', 'dispatch-nested.json', 'injection.json'] as $name) {
            $json     = $this->loadFixture($name);
            $env1     = parseEnvelope($json);
            $rendered = serializeEnvelope($env1);
            $env2     = parseEnvelope($rendered);
            $this->assertEquals(
                json_decode($rendered, true),
                json_decode(serializeEnvelope($env2), true),
                sprintf('round-trip mismatch for %s', $name),
            );
        }
    }

    public function testDispatchFixtureCarriesProvenanceFields(): void
    {
        $env = parseEnvelope($this->loadFixture('dispatch.json'));
        // Turn 1 is the orchestrator's tool_call dispatching A.
        $this->assertSame('C', $env->turns[1]->agentId);
        $part0 = $env->turns[1]->content[0];
        $this->assertInstanceOf(ToolCallPart::class, $part0);

        // Turn 3 is the orchestrator's mid-flight interjection while A's call is open.
        $this->assertSame('call_C_dispatch_A', $env->turns[3]->inReplyToCallID ?? $env->turns[3]->inReplyToCallId);

        // Turn 4 is A dispatching B with parent_call_id pointing at C's open call.
        $part4 = $env->turns[4]->content[0];
        $this->assertInstanceOf(ToolCallPart::class, $part4);
        $this->assertSame('call_C_dispatch_A', $part4->parentCallId);
    }

    // ---- Envelope-level validation (§7.2 / §7.3) ---------------------------

    public function testDispatchedFromMutuallyExclusiveWithForkFields(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [],
            parentId: 'parent',
            forkPoint: 0,
            dispatchedFrom: new DispatchedFrom('p', 'c'),
        );
        $msgs = array_map(static fn ($e): string => (string) $e, validate($env));
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'mutually exclusive')),
            'got: ' . implode(' | ', $msgs),
        );
    }

    public function testDispatchedFromRequiresBothMembers(): void
    {
        // Empty callId triggers requires-both diagnostic.
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [],
            dispatchedFrom: new DispatchedFrom('p', ''),
        );
        $msgs = array_map(static fn ($e): string => (string) $e, validate($env));
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'envelope_id and call_id')),
            'got: ' . implode(' | ', $msgs),
        );
    }

    public function testDispatchedFromAloneAccepted(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [],
            dispatchedFrom: new DispatchedFrom('parent-env', 'call-1'),
        );
        $this->assertSame([], validate($env));
    }

    public function testInjectedTurnsRequireAllThreeIDs(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [],
            injectedTurns: [new InjectedTurnRef('src', '', '')],
        );
        $msgs = array_map(static fn ($e): string => (string) $e, validate($env));
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'injected_turns')),
            'got: ' . implode(' | ', $msgs),
        );
    }

    public function testInjectedAfterTurnIdMustExistInEnvelope(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 't-1',
                    role: RoleEnum::User,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new TextPart('hi')],
                ),
            ],
            injectedTurns: [
                new InjectedTurnRef('src', 's', 's', injectedAfterTurnId: 'does-not-exist'),
            ],
        );
        $msgs = array_map(static fn ($e): string => (string) $e, validate($env));
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'injected_after_turn_id')),
            'got: ' . implode(' | ', $msgs),
        );
    }

    public function testInjectedTurnsCreationTimeAccepted(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [],
            injectedTurns: [new InjectedTurnRef('src', 'a', 'b')],
        );
        $this->assertSame([], validate($env));
    }

    // ---- tool_call.parent_call_id (§6.2) -----------------------------------

    public function testParentCallIdRequiresOpenOuterCall(): void
    {
        // Inner tool_call references an outer that was never seen.
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 'a',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolCallPart('inner', 'echo', [], parentCallId: 'outer')],
                ),
            ],
        );
        $msgs = array_map(static fn ($e): string => (string) $e, validate($env));
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'parent_call_id')),
            'got: ' . implode(' | ', $msgs),
        );
    }

    public function testParentCallIdAcceptedWhenOuterOpen(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 'a',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolCallPart('outer', 'dispatch', [])],
                ),
                new Turn(
                    id: 'b',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolCallPart('inner', 'sub', [], parentCallId: 'outer')],
                ),
            ],
        );
        $this->assertSame([], validate($env));
    }

    public function testParentCallIdRejectedAfterOuterResolved(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 'a',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolCallPart('outer', 'dispatch', [])],
                ),
                new Turn(
                    id: 't',
                    role: RoleEnum::Tool,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolResultPart('outer', 'done')],
                ),
                new Turn(
                    id: 'b',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolCallPart('inner', 'sub', [], parentCallId: 'outer')],
                ),
            ],
        );
        $msgs = array_map(static fn ($e): string => (string) $e, validate($env));
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'parent_call_id')),
            'got: ' . implode(' | ', $msgs),
        );
    }

    // ---- Turn.in_reply_to_call_id (§3.2) ----------------------------------

    public function testInReplyToCallIdRequiresOpenCall(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 'u',
                    role: RoleEnum::User,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new TextPart('interjection')],
                    inReplyToCallId: 'missing',
                ),
            ],
        );
        $msgs = array_map(static fn ($e): string => (string) $e, validate($env));
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'in_reply_to_call_id')),
            'got: ' . implode(' | ', $msgs),
        );
    }

    public function testInReplyToCallIdForbiddenOnToolRole(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 'a',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolCallPart('c-1', 'x', [])],
                ),
                new Turn(
                    id: 't',
                    role: RoleEnum::Tool,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolResultPart('c-1', 'out')],
                    inReplyToCallId: 'c-1',
                ),
            ],
        );
        $msgs = array_map(static fn ($e): string => (string) $e, validate($env));
        $this->assertNotEmpty(
            array_filter($msgs, static fn (string $m): bool => str_contains($m, 'in_reply_to_call_id')),
            'got: ' . implode(' | ', $msgs),
        );
    }

    public function testInReplyToCallIdAcceptedWhileCallOpen(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 'a',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolCallPart('c-1', 'dispatch', [])],
                ),
                new Turn(
                    id: 'u',
                    role: RoleEnum::User,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new TextPart('update')],
                    inReplyToCallId: 'c-1',
                ),
                new Turn(
                    id: 't',
                    role: RoleEnum::Tool,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolResultPart('c-1', 'out')],
                ),
            ],
        );
        $this->assertSame([], validate($env));
    }

    // ---- agent_id + child_envelope_id round-trip --------------------------

    public function testAcceptsAgentIdOnApplicableRoles(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 'a',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new TextPart('hi')],
                    agentId: 'C',
                ),
            ],
        );
        $this->assertSame([], validate($env));
        $this->assertStringContainsString('"agent_id":"C"', serializeEnvelope($env));
    }

    public function testChildEnvelopeIdRoundTripsOnToolResult(): void
    {
        $env = new Envelope(
            crtxVersion: '0.1',
            id: 'x',
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            source: Source::default(),
            turns: [
                new Turn(
                    id: 'a',
                    role: RoleEnum::Assistant,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolCallPart('c-1', 'dispatch', [])],
                ),
                new Turn(
                    id: 't',
                    role: RoleEnum::Tool,
                    createdAt: '2026-01-01T00:00:00Z',
                    content: [new ToolResultPart('c-1', 'done', childEnvelopeId: 'child-env')],
                ),
            ],
        );
        $this->assertSame([], validate($env));
        $this->assertStringContainsString('"child_envelope_id":"child-env"', serializeEnvelope($env));
    }

    private function loadFixture(string $name): string
    {
        $path = __DIR__ . '/testdata/crtx_v0.1/' . $name;
        $data = file_get_contents($path);
        $this->assertNotFalse($data, "fixture missing: $path");

        return $data;
    }
}
