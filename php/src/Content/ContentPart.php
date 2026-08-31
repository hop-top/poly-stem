<?php

declare(strict_types=1);

namespace HopTop\Stem\Content;

use JsonSerializable;

/**
 * Discriminated union on the `type` discriminator.
 *
 * v0.1 variants:
 *  - {@see TextPart}        — type = "text"
 *  - {@see ToolCallPart}    — type = "tool_call"
 *  - {@see ToolResultPart}  — type = "tool_result"
 *  - {@see ImagePart}       — type = "image"
 *  - {@see ThinkingPart}    — type = "thinking"
 *  - {@see ExtensionPart}   — type prefixed "x-"
 *
 * Each concrete class is a readonly PHP value type with a public
 * `metadata` map (free-form, omitted from wire when empty).
 */
interface ContentPart extends JsonSerializable
{
    /** crtx v0.1 type discriminator. */
    public function type(): string;

    /** @return array<string, mixed> Always set; may be empty. */
    public function metadata(): array;
}
