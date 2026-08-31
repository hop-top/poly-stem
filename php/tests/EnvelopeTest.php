<?php

declare(strict_types=1);

namespace HopTop\Stem\Tests;

use HopTop\Stem\Content\ExtensionPart;
use HopTop\Stem\Content\ImagePart;
use HopTop\Stem\Content\TextPart;
use HopTop\Stem\Content\ThinkingPart;
use HopTop\Stem\Content\ToolCallPart;
use HopTop\Stem\Content\ToolResultPart;
use HopTop\Stem\Envelope;
use HopTop\Stem\EnvelopeParseException;
use HopTop\Stem\RoleEnum;
use HopTop\Stem\Source;
use HopTop\Stem\Stem;
use HopTop\Stem\Turn;
use PHPUnit\Framework\TestCase;

use function HopTop\Stem\parseEnvelope;
use function HopTop\Stem\serializeEnvelope;
use function HopTop\Stem\validate;
use function HopTop\Stem\validateBytes;

final class EnvelopeTest extends TestCase
{
    public function testStemConstantsMatchSpec(): void
    {
        $this->assertSame('0.1', Stem::CRTX_VERSION);
        $this->assertSame('stem', Stem::SOURCE_KIND);
        $this->assertNotSame('', Stem::VERSION);
    }

    public function testRoleEnumStringBackings(): void
    {
        $this->assertSame('user', RoleEnum::User->value);
        $this->assertSame('assistant', RoleEnum::Assistant->value);
        $this->assertSame('tool', RoleEnum::Tool->value);
        $this->assertSame('system', RoleEnum::System->value);
        $this->assertSame('developer', RoleEnum::Developer->value);
    }

    public function testSourceDefault(): void
    {
        $s = Source::default();
        $this->assertSame('stem', $s->kind);
        $this->assertSame(Stem::VERSION, $s->version);
        $this->assertNull($s->instance);
    }

    public function testEnvelopeFreshHasEmptyTurnsAndSerializesArray(): void
    {
        $env = Envelope::fresh('e-1');
        $this->assertSame([], $env->turns);

        $json = serializeEnvelope($env);
        $this->assertStringContainsString('"turns":[]', $json, 'empty turns must serialize to [], not null');
    }

    public function testTextPartRoundTrip(): void
    {
        $part = new TextPart('hello');
        $this->assertSame('text', $part->type());
        $this->assertSame(['type' => 'text', 'text' => 'hello'], $part->jsonSerialize());
    }

    public function testToolCallPartEmptyInputBecomesObject(): void
    {
        $part = new ToolCallPart('c1', 'fn', []);
        $encoded = json_encode($part);
        $this->assertIsString($encoded);
        $this->assertStringContainsString('"input":{}', $encoded);
    }

    public function testImagePartConstructorRejectsBothAndNeither(): void
    {
        $this->expectException(EnvelopeParseException::class);
        new ImagePart('image/png');
    }

    public function testImagePartConstructorRejectsBoth(): void
    {
        $this->expectException(EnvelopeParseException::class);
        new ImagePart('image/png', data: 'abc', url: 'https://x/');
    }

    public function testImagePartUrlAccepted(): void
    {
        $part = new ImagePart('image/png', url: 'https://example.com/a.png');
        $encoded = $part->jsonSerialize();
        $this->assertSame('image', $encoded['type']);
        $this->assertSame('https://example.com/a.png', $encoded['url']);
        $this->assertArrayNotHasKey('data', $encoded);
    }

    public function testParseEnvelopeMinimal(): void
    {
        $json = $this->loadFixture('minimal.json');
        $env  = parseEnvelope($json);

        $this->assertSame('0.1', $env->crtxVersion);
        $this->assertSame('01JCRTX0EXAMPLEMINIMAL00001', $env->id);
        $this->assertCount(2, $env->turns);
        $this->assertSame(RoleEnum::User, $env->turns[0]->role);
        $this->assertSame(RoleEnum::Assistant, $env->turns[1]->role);
        $this->assertInstanceOf(TextPart::class, $env->turns[0]->content[0]);

        $this->assertSame([], validate($env));
    }

    public function testParseEnvelopeToolCall(): void
    {
        $json = $this->loadFixture('tool-call.json');
        $env  = parseEnvelope($json);

        $this->assertCount(4, $env->turns);
        $this->assertInstanceOf(ToolCallPart::class, $env->turns[1]->content[0]);
        $this->assertInstanceOf(ToolResultPart::class, $env->turns[2]->content[0]);
        $this->assertSame('host-laptop/pid-4711', $env->source->instance);
        $this->assertSame([], validate($env));
    }

    public function testParseEnvelopeFork(): void
    {
        $json = $this->loadFixture('fork.json');
        $env  = parseEnvelope($json);

        $this->assertSame('01JCRTX0EXAMPLEFORK00PARENT1', $env->parentId);
        $this->assertSame(1, $env->forkPoint);
        $this->assertSame(
            'user requested alternate phrasing',
            $env->metadata['io.jadb.fork_reason'] ?? null,
        );
        $this->assertSame([], validate($env));
    }

    /**
     * Round-trip: parse → serialize → reparse → deep-assert structural
     * equality. Whitespace/ordering may differ; structural JSON equality
     * is the contract.
     */
    public function testRoundTripAllFixtures(): void
    {
        foreach (['minimal.json', 'tool-call.json', 'fork.json'] as $name) {
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

    public function testExtensionPartRoundTripVerbatim(): void
    {
        $env = Envelope::fresh('e-ext');
        $env = $env->appendTurn(new Turn(
            id: 't0',
            role: RoleEnum::Assistant,
            createdAt: '2026-01-01T00:00:00Z',
            content: [
                new ExtensionPart('x-io.jadb.custom', [
                    'type'          => 'x-io.jadb.custom',
                    'custom_field'  => 42,
                    'nested'        => ['a' => 1],
                ]),
            ],
        ));
        $json = serializeEnvelope($env);
        $reloaded = parseEnvelope($json);
        $part = $reloaded->turns[0]->content[0];
        $this->assertInstanceOf(ExtensionPart::class, $part);
        $this->assertSame('x-io.jadb.custom', $part->type);
        $this->assertSame(42, $part->raw['custom_field']);
        $this->assertSame(1, $part->raw['nested']['a']);
    }

    public function testThinkingPartSerializesSignature(): void
    {
        $part = new ThinkingPart('reasoning trace', 'sig-abc');
        $arr = $part->jsonSerialize();
        $this->assertSame('thinking', $arr['type']);
        $this->assertSame('reasoning trace', $arr['text']);
        $this->assertSame('sig-abc', $arr['signature']);
    }

    public function testValidateBytesAcceptsValidEnvelope(): void
    {
        $errors = validateBytes($this->loadFixture('minimal.json'));
        $this->assertSame([], $errors);
    }

    private function loadFixture(string $name): string
    {
        $path = __DIR__ . '/testdata/crtx_v0.1/' . $name;
        $data = file_get_contents($path);
        $this->assertNotFalse($data, "fixture missing: $path");

        return $data;
    }
}
