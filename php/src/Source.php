<?php

declare(strict_types=1);

namespace HopTop\Stem;

use JsonSerializable;

/**
 * Identifies the runtime that produced the Envelope.
 *
 * `kind` and `version` are required; `instance` is free-form (hostname,
 * pid, session id) and is omitted from the wire when empty.
 */
final class Source implements JsonSerializable
{
    public function __construct(
        public readonly string $kind,
        public readonly string $version,
        public readonly ?string $instance = null,
    ) {
    }

    public static function default(): self
    {
        return new self(Stem::SOURCE_KIND, Stem::VERSION);
    }

    /** @param array<string, mixed> $data */
    public static function fromArray(array $data): self
    {
        $kind = $data['kind'] ?? null;
        if (!is_string($kind) || $kind === '') {
            throw new EnvelopeParseException('source.kind must be a non-empty string');
        }
        $version = $data['version'] ?? null;
        if (!is_string($version) || $version === '') {
            throw new EnvelopeParseException('source.version must be a non-empty string');
        }
        $instance = $data['instance'] ?? null;
        if ($instance !== null && !is_string($instance)) {
            throw new EnvelopeParseException('source.instance must be a string when present');
        }

        return new self($kind, $version, $instance === '' ? null : $instance);
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        $out = [
            'kind'    => $this->kind,
            'version' => $this->version,
        ];
        if ($this->instance !== null && $this->instance !== '') {
            $out['instance'] = $this->instance;
        }

        return $out;
    }
}
