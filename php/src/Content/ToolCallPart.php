<?php

declare(strict_types=1);

namespace HopTop\Stem\Content;

final class ToolCallPart implements ContentPart
{
    /**
     * @param array<string, mixed>|string $input Arguments. Object form is
     *   canonical; string form is permitted for legacy / streaming chunks.
     * @param array<string, mixed>        $metadata
     */
    public function __construct(
        public readonly string $callId,
        public readonly string $name,
        public readonly array|string $input,
        public readonly array $metadata = [],
        public readonly ?string $parentCallId = null,
    ) {
    }

    public function type(): string
    {
        return 'tool_call';
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
            'type'    => 'tool_call',
            'call_id' => $this->callId,
            'name'    => $this->name,
            // Empty PHP arrays serialize to `[]`; force `{}` when input
            // is the canonical object form and happens to be empty.
            'input'   => is_array($this->input) && $this->input === []
                ? new \stdClass()
                : $this->input,
        ];
        if ($this->parentCallId !== null && $this->parentCallId !== '') {
            $out['parent_call_id'] = $this->parentCallId;
        }
        if ($this->metadata !== []) {
            $out['metadata'] = $this->metadata;
        }

        return $out;
    }
}
