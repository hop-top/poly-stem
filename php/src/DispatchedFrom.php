<?php

declare(strict_types=1);

namespace HopTop\Stem;

use JsonSerializable;

/**
 * Marks an Envelope as a dispatch nest spawned by an open tool_call in
 * another Envelope. Both members are required by the schema; mutually
 * exclusive with parentId/forkPoint. See envelope.md §7.2.
 */
final class DispatchedFrom implements JsonSerializable
{
    public function __construct(
        public readonly string $envelopeId,
        public readonly string $callId,
    ) {
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        return [
            'envelope_id' => $this->envelopeId,
            'call_id'     => $this->callId,
        ];
    }
}
