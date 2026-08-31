<?php

declare(strict_types=1);

namespace HopTop\Stem\Tests;

use HopTop\Stem\Content\ToolResultPart;
use PHPUnit\Framework\TestCase;

/**
 * crtx v0.1 ToolResultPart.jsonSerialize tests.
 *
 * Spec: tool_result.output is typed `any` — producer intent (list vs
 * object vs scalar) must round-trip verbatim. Empty list MUST emit []
 * on the wire, not {}.
 */
final class ToolResultPartTest extends TestCase
{
    public function testEmptyArrayOutputSerializesAsJsonArray(): void
    {
        $part = new ToolResultPart('c-1', []);
        $json = json_encode($part->jsonSerialize());
        $this->assertNotFalse($json);
        $this->assertStringContainsString('"output":[]', $json);
        $this->assertStringNotContainsString('"output":{}', $json);
    }

    public function testAssociativeArrayOutputSerializesAsJsonObject(): void
    {
        $part = new ToolResultPart('c-1', ['temp_c' => 18]);
        $json = json_encode($part->jsonSerialize());
        $this->assertNotFalse($json);
        $this->assertStringContainsString('"output":{"temp_c":18}', $json);
    }

    public function testListArrayOutputSerializesAsJsonArray(): void
    {
        $part = new ToolResultPart('c-1', [1, 2, 3]);
        $json = json_encode($part->jsonSerialize());
        $this->assertNotFalse($json);
        $this->assertStringContainsString('"output":[1,2,3]', $json);
    }

    public function testStringOutputPassesThrough(): void
    {
        $part = new ToolResultPart('c-1', 'done');
        $json = json_encode($part->jsonSerialize());
        $this->assertNotFalse($json);
        $this->assertStringContainsString('"output":"done"', $json);
    }

    public function testNullOutputPassesThrough(): void
    {
        $part = new ToolResultPart('c-1', null);
        $json = json_encode($part->jsonSerialize());
        $this->assertNotFalse($json);
        $this->assertStringContainsString('"output":null', $json);
    }

    public function testEmptyArrayOutputRoundTrips(): void
    {
        $json = json_encode([
            'crtx_version' => '0.1',
            'id' => 'env-1',
            'created_at' => '2026-01-01T00:00:00Z',
            'updated_at' => '2026-01-01T00:00:00Z',
            'source' => ['kind' => 'test', 'version' => '0.0'],
            'turns' => [[
                'id' => 't-0',
                'role' => 'tool',
                'created_at' => '2026-01-01T00:00:00Z',
                'content' => [['type' => 'tool_result', 'call_id' => 'c-1', 'output' => []]],
            ]],
        ]);
        $this->assertNotFalse($json);
        $env = \HopTop\Stem\parseEnvelope($json);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"output":[]', $reserialized);
        $this->assertStringNotContainsString('"output":{}', $reserialized);
    }

    /**
     * Producer-sent JSON `"output":{}` MUST round-trip as `"output":{}`,
     * not collapse to `"output":[]`. PHP's `json_decode(..., true)`
     * loses the empty-object distinction in assoc mode; the parser
     * recovers it by also decoding in object mode and consulting that
     * counterpart at the tool_result.output position.
     */
    public function testEmptyObjectOutputRoundTrips(): void
    {
        // Build the envelope JSON by hand so we can pin `"output":{}`
        // literally — json_encode([]) emits [], so we can't go through
        // an assoc-array fixture.
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":{}}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"output":{}', $reserialized);
        $this->assertStringNotContainsString('"output":[]', $reserialized);
    }

    /**
     * Sanity check: non-empty object output ("output":{"k":"v"}) must
     * still round-trip as a JSON object. PHP's assoc decode treats
     * string-keyed maps as PHP associative arrays, which json_encode
     * naturally emits as objects — the fix MUST NOT regress this path.
     */
    public function testNonEmptyObjectOutputRoundTrips(): void
    {
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":{"temp_c":20}}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"output":{"temp_c":20}', $reserialized);
    }

    /**
     * Regression guard: null tool_result.output must pass through
     * untouched. Confirms the empty-object recovery does not fire on
     * null (recovery requires both assoc===[] and objRaw->output as
     * stdClass; null fails both predicates).
     */
    public function testNullOutputRoundTrips(): void
    {
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":null}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"output":null', $reserialized);
    }

    /**
     * Regression guard: numeric-keyed list output must round-trip as a
     * JSON array, not get rewritten to an object. The empty-object
     * recovery only fires when assoc===[]; non-empty lists are passed
     * through verbatim.
     */
    public function testListOutputRoundTrips(): void
    {
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":[1,2,3]}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"output":[1,2,3]', $reserialized);
        $this->assertStringNotContainsString('"output":{', $reserialized);
    }

    /**
     * Nested empty `{}` inside tool_result.output must round-trip as
     * `{}`, not collapse to `[]`. The recovery walks the assoc tree in
     * lockstep with the object-mode counterpart so empty objects at any
     * depth survive.
     */
    public function testNestedEmptyObjectRoundTrips(): void
    {
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":{"nested":{"empty":{}}}}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"output":{"nested":{"empty":{}}}', $reserialized);
        $this->assertStringNotContainsString('"empty":[]', $reserialized);
    }

    /**
     * Single-key nested empty object: `{"data":{}}` must survive
     * unchanged. Smallest case that exercises the recursive walk past
     * the top-level output position.
     */
    public function testNestedEmptyObjectAtKeyRoundTrips(): void
    {
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":{"data":{}}}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"output":{"data":{}}', $reserialized);
        $this->assertStringNotContainsString('"data":[]', $reserialized);
    }

    /**
     * Sibling empty values: `{"data":{},"items":[]}` must round-trip
     * with `data` as `{}` (was object in source) AND `items` as `[]`
     * (was array in source). Proves the walker distinguishes the two
     * via the object-mode counterpart and does NOT over-correct real
     * empty arrays into objects.
     */
    public function testNestedEmptyObjectAndArrayMixedRoundTrips(): void
    {
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":{"data":{},"items":[]}}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"data":{}', $reserialized);
        $this->assertStringContainsString('"items":[]', $reserialized);
        $this->assertStringNotContainsString('"data":[]', $reserialized);
        $this->assertStringNotContainsString('"items":{}', $reserialized);
    }

    /**
     * Empty object inside a list element: `{"results":[{"meta":{}}]}`
     * exercises the positional list-recursion branch. The walker must
     * descend into list[0] and then into its `meta` key.
     */
    public function testNestedEmptyObjectInsideListRoundTrips(): void
    {
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":{"results":[{"meta":{}}]}}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"output":{"results":[{"meta":{}}]}', $reserialized);
        $this->assertStringNotContainsString('"meta":[]', $reserialized);
    }

    /**
     * Combined coverage: `{"data":{},"results":[{"meta":{},"items":[]}]}`.
     * Mirrors the worked example from the task description. Confirms:
     *  - top-level `data` survives as `{}`
     *  - nested `meta` survives as `{}` inside a list element
     *  - real empty array `items` stays `[]`
     *  - no over-correction anywhere
     */
    public function testNestedEmptyObjectDeepCombinedRoundTrips(): void
    {
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":{"data":{},"results":[{"meta":{},"items":[]}]}}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString(
            '"output":{"data":{},"results":[{"meta":{},"items":[]}]}',
            $reserialized,
        );
    }

    /**
     * Regression guard: a NON-empty object containing both empty `{}`
     * and empty `[]` values MUST NOT over-correct. The non-empty outer
     * survives as an object, the inner `{}` is restored, and the inner
     * `[]` stays `[]`. Catches any walker that confuses obj-mode
     * stdClass with a list counterpart or vice versa.
     */
    public function testNonEmptyObjectWithMixedEmptiesNoOverCorrection(): void
    {
        $envJson = '{'
            . '"crtx_version":"0.1",'
            . '"id":"env-1",'
            . '"created_at":"2026-01-01T00:00:00Z",'
            . '"updated_at":"2026-01-01T00:00:00Z",'
            . '"source":{"kind":"test","version":"0.0"},'
            . '"turns":[{'
            .   '"id":"t-0",'
            .   '"role":"tool",'
            .   '"created_at":"2026-01-01T00:00:00Z",'
            .   '"content":[{"type":"tool_result","call_id":"c-1","output":{"temp_c":20,"flags":{},"tags":[]}}]'
            . '}]'
            . '}';
        $env = \HopTop\Stem\parseEnvelope($envJson);
        $reserialized = json_encode($env);
        $this->assertNotFalse($reserialized);
        $this->assertStringContainsString('"temp_c":20', $reserialized);
        $this->assertStringContainsString('"flags":{}', $reserialized);
        $this->assertStringContainsString('"tags":[]', $reserialized);
        $this->assertStringNotContainsString('"flags":[]', $reserialized);
        $this->assertStringNotContainsString('"tags":{}', $reserialized);
    }
}
