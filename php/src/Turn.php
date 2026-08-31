<?php

declare(strict_types=1);

namespace HopTop\Stem;

use HopTop\Stem\Content\ContentPart;
use JsonSerializable;

/**
 * One contribution to a conversation. Ordered by position in
 * Envelope::$turns, not by createdAt.
 *
 * $agentId and $inReplyToCallId are crtx v0.1 multi-agent provenance
 * fields. See envelope.md §3.1 and §3.2 in the spec.
 */
final class Turn implements JsonSerializable
{
    /**
     * @param list<ContentPart>   $content  MUST contain >=1 part per spec.
     * @param array<string,mixed> $metadata Free-form. Omitted from wire when empty.
     */
    public function __construct(
        public readonly string $id,
        public readonly RoleEnum $role,
        public readonly string $createdAt,
        public readonly array $content,
        public readonly array $metadata = [],
        public readonly ?string $agentId = null,
        public readonly ?string $inReplyToCallId = null,
    ) {
    }

    /** @return array<string, mixed> */
    public function jsonSerialize(): array
    {
        $out = [
            'id'         => $this->id,
            'role'       => $this->role->value,
            'created_at' => $this->createdAt,
            'content'    => array_map(
                static fn (ContentPart $p): mixed => $p->jsonSerialize(),
                $this->content,
            ),
        ];
        if ($this->agentId !== null && $this->agentId !== '') {
            $out['agent_id'] = $this->agentId;
        }
        if ($this->inReplyToCallId !== null && $this->inReplyToCallId !== '') {
            $out['in_reply_to_call_id'] = $this->inReplyToCallId;
        }
        if ($this->metadata !== []) {
            $out['metadata'] = $this->metadata;
        }

        return $out;
    }
}
