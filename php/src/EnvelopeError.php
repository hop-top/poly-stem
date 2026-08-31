<?php

declare(strict_types=1);

namespace HopTop\Stem;

/**
 * One structural error reported by {@see validate()}.
 *
 * `path` is a dotted/bracketed JSON Pointer-ish locator
 * (e.g. `turns[0].content[1]`). `message` is human-readable.
 */
final class EnvelopeError
{
    public function __construct(
        public readonly string $path,
        public readonly string $message,
    ) {
    }

    public function __toString(): string
    {
        return $this->path === ''
            ? $this->message
            : sprintf('%s: %s', $this->path, $this->message);
    }
}
