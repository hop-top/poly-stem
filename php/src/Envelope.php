<?php

declare(strict_types=1);

namespace HopTop\Stem;

use JsonSerializable;
use stdClass;

/**
 * crtx v0.1 Envelope — the top-level container for an AI agent
 * conversation.
 *
 * Mirrors the Go {@see https://github.com/hop-top/poly-stem/blob/main/go/envelope.go}
 * Session struct on the wire. The PHP value type is intentionally
 * immutable: appends produce a new Envelope rather than mutating.
 */
final class Envelope implements JsonSerializable
{
    /**
     * @param list<Turn>            $turns
     * @param array<string, mixed>  $metadata
     * @param list<InjectedTurnRef> $injectedTurns
     */
    public function __construct(
        public readonly string $crtxVersion,
        public readonly string $id,
        public readonly string $createdAt,
        public readonly string $updatedAt,
        public readonly Source $source,
        public readonly array $turns,
        public readonly ?string $parentId = null,
        public readonly ?int $forkPoint = null,
        public readonly array $metadata = [],
        public readonly ?DispatchedFrom $dispatchedFrom = null,
        public readonly array $injectedTurns = [],
    ) {
    }

    /**
     * Construct a fresh stem-shaped Envelope with no turns and the
     * default stem Source. Timestamps are now (UTC, ISO 8601 with `Z`
     * suffix).
     */
    public static function fresh(string $id, ?Source $source = null): self
    {
        $now = (new \DateTimeImmutable('now', new \DateTimeZone('UTC')))
            ->format('Y-m-d\TH:i:s\Z');

        return new self(
            crtxVersion: Stem::CRTX_VERSION,
            id: $id,
            createdAt: $now,
            updatedAt: $now,
            source: $source ?? Source::default(),
            turns: [],
        );
    }

    /** Return a new Envelope with the given Turn appended and updatedAt bumped. */
    public function appendTurn(Turn $turn): self
    {
        $turns = $this->turns;
        $turns[] = $turn;
        $now = (new \DateTimeImmutable('now', new \DateTimeZone('UTC')))
            ->format('Y-m-d\TH:i:s\Z');

        return new self(
            crtxVersion: $this->crtxVersion,
            id: $this->id,
            createdAt: $this->createdAt,
            updatedAt: $now,
            source: $this->source,
            turns: $turns,
            parentId: $this->parentId,
            forkPoint: $this->forkPoint,
            metadata: $this->metadata,
            dispatchedFrom: $this->dispatchedFrom,
            injectedTurns: $this->injectedTurns,
        );
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        $out = [
            'crtx_version' => $this->crtxVersion,
            'id'           => $this->id,
            'created_at'   => $this->createdAt,
            'updated_at'   => $this->updatedAt,
            'source'       => $this->source->jsonSerialize(),
            // Empty turns array MUST serialize as JSON [], NOT null.
            'turns'        => array_map(
                static fn (Turn $t): array => $t->jsonSerialize(),
                $this->turns,
            ),
        ];
        if ($this->parentId !== null && $this->parentId !== '') {
            $out['parent_id'] = $this->parentId;
        }
        if ($this->forkPoint !== null) {
            $out['fork_point'] = $this->forkPoint;
        }
        if ($this->dispatchedFrom !== null) {
            $out['dispatched_from'] = $this->dispatchedFrom->jsonSerialize();
        }
        if ($this->injectedTurns !== []) {
            $out['injected_turns'] = array_map(
                static fn (InjectedTurnRef $r): array => $r->jsonSerialize(),
                $this->injectedTurns,
            );
        }
        if ($this->metadata !== []) {
            $out['metadata'] = $this->metadata;
        }

        return $out;
    }

    /**
     * Force JSON encoder to emit a real `{}` for empty object-shaped
     * metadata. Helper for sample code that explicitly wants object form.
     */
    public static function emptyObject(): stdClass
    {
        return new stdClass();
    }
}
