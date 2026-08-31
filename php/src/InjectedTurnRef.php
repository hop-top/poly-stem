<?php

declare(strict_types=1);

namespace HopTop\Stem;

use JsonSerializable;

/**
 * References a slice of Turns in another Envelope to be included as
 * part of this Envelope's context. Append-only. See envelope.md §7.3.
 */
final class InjectedTurnRef implements JsonSerializable
{
    public function __construct(
        public readonly string $envelopeId,
        public readonly string $startTurnId,
        public readonly string $endTurnId,
        public readonly ?string $injectedAfterTurnId = null,
    ) {
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        $out = [
            'envelope_id'   => $this->envelopeId,
            'start_turn_id' => $this->startTurnId,
            'end_turn_id'   => $this->endTurnId,
        ];
        if ($this->injectedAfterTurnId !== null && $this->injectedAfterTurnId !== '') {
            $out['injected_after_turn_id'] = $this->injectedAfterTurnId;
        }

        return $out;
    }
}
