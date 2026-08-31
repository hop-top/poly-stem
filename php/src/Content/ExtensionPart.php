<?php

declare(strict_types=1);

namespace HopTop\Stem\Content;

/**
 * Preserves an "x-"-prefixed ContentPart verbatim.
 *
 * Consumers encountering an unknown `type` MUST preserve the part
 * verbatim when forwarding the Envelope, per crtx v0.1 §6.6.
 * `$raw` holds the entire decoded JSON object for this part, including
 * the `type` discriminator and any extension-specific fields.
 */
final class ExtensionPart implements ContentPart
{
    /**
     * @param string                $type Type discriminator. MUST start with "x-".
     * @param array<string, mixed>  $raw  Decoded JSON object for the part, verbatim.
     */
    public function __construct(
        public readonly string $type,
        public readonly array $raw,
    ) {
    }

    public function type(): string
    {
        return $this->type;
    }

    /** @return array<string, mixed> */
    public function metadata(): array
    {
        /** @var array<string, mixed> $meta */
        $meta = $this->raw['metadata'] ?? [];

        return is_array($meta) ? $meta : [];
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        $out = $this->raw;
        // Ensure the discriminator is always present and matches the
        // declared type, even if a caller mutated raw post-construction.
        $out['type'] = $this->type;

        return $out;
    }
}
