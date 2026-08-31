<?php

declare(strict_types=1);

namespace HopTop\Stem\Content;

final class ToolResultPart implements ContentPart
{
    /**
     * @param mixed                $output  Any JSON-serializable value
     *                                       (including null).
     * @param array<string, mixed> $metadata
     */
    public function __construct(
        public readonly string $callId,
        public readonly mixed $output,
        public readonly bool $isError = false,
        public readonly array $metadata = [],
        public readonly ?string $childEnvelopeId = null,
    ) {
    }

    public function type(): string
    {
        return 'tool_result';
    }

    /** @return array<string, mixed> */
    public function metadata(): array
    {
        return $this->metadata;
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        $out = [
            'type'    => 'tool_result',
            'call_id' => $this->callId,
            'output'  => $this->output,
        ];
        if ($this->isError) {
            $out['is_error'] = true;
        }
        if ($this->childEnvelopeId !== null && $this->childEnvelopeId !== '') {
            $out['child_envelope_id'] = $this->childEnvelopeId;
        }
        if ($this->metadata !== []) {
            $out['metadata'] = $this->metadata;
        }

        return $out;
    }
}
