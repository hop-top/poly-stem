<?php

declare(strict_types=1);

namespace HopTop\Stem\Content;

use HopTop\Stem\EnvelopeParseException;

final class ImagePart implements ContentPart
{
    /**
     * Exactly one of `$data` or `$url` MUST be a non-empty string. The
     * constructor enforces the XOR so callers cannot construct an
     * invalid value type that would later fail validation.
     *
     * @param array<string, mixed> $metadata
     */
    public function __construct(
        public readonly string $mime,
        public readonly ?string $data = null,
        public readonly ?string $url = null,
        public readonly ?string $alt = null,
        public readonly array $metadata = [],
    ) {
        $hasData = $data !== null && $data !== '';
        $hasUrl  = $url !== null && $url !== '';
        if ($hasData === $hasUrl) {
            throw new EnvelopeParseException(
                'image part: exactly one of data/url must be set'
            );
        }
    }

    public function type(): string
    {
        return 'image';
    }

    /** @return array<string, mixed> */
    public function metadata(): array
    {
        return $this->metadata;
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        $out = ['type' => 'image', 'mime' => $this->mime];
        if ($this->data !== null && $this->data !== '') {
            $out['data'] = $this->data;
        }
        if ($this->url !== null && $this->url !== '') {
            $out['url'] = $this->url;
        }
        if ($this->alt !== null && $this->alt !== '') {
            $out['alt'] = $this->alt;
        }
        if ($this->metadata !== []) {
            $out['metadata'] = $this->metadata;
        }

        return $out;
    }
}
