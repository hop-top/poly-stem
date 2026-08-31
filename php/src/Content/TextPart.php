<?php

declare(strict_types=1);

namespace HopTop\Stem\Content;

final class TextPart implements ContentPart
{
    /** @param array<string, mixed> $metadata */
    public function __construct(
        public readonly string $text,
        public readonly array $metadata = [],
    ) {
    }

    public function type(): string
    {
        return 'text';
    }

    /** @return array<string, mixed> */
    public function metadata(): array
    {
        return $this->metadata;
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        $out = ['type' => 'text', 'text' => $this->text];
        if ($this->metadata !== []) {
            $out['metadata'] = $this->metadata;
        }

        return $out;
    }
}
