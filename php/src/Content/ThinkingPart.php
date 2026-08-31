<?php

declare(strict_types=1);

namespace HopTop\Stem\Content;

final class ThinkingPart implements ContentPart
{
    /** @param array<string, mixed> $metadata */
    public function __construct(
        public readonly string $text,
        public readonly ?string $signature = null,
        public readonly array $metadata = [],
    ) {
    }

    public function type(): string
    {
        return 'thinking';
    }

    /** @return array<string, mixed> */
    public function metadata(): array
    {
        return $this->metadata;
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        $out = ['type' => 'thinking', 'text' => $this->text];
        if ($this->signature !== null && $this->signature !== '') {
            $out['signature'] = $this->signature;
        }
        if ($this->metadata !== []) {
            $out['metadata'] = $this->metadata;
        }

        return $out;
    }
}
