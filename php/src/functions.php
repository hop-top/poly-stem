<?php

declare(strict_types=1);

namespace HopTop\Stem;

use HopTop\Stem\Content\ContentPart;
use HopTop\Stem\Content\ExtensionPart;
use HopTop\Stem\Content\ImagePart;
use HopTop\Stem\Content\TextPart;
use HopTop\Stem\Content\ThinkingPart;
use HopTop\Stem\Content\ToolCallPart;
use HopTop\Stem\Content\ToolResultPart;
use JsonException;

/**
 * Allowed top-level Envelope fields (additionalProperties: false).
 *
 * @var array<string, true>
 */
const ENVELOPE_FIELDS = [
    'crtx_version'    => true,
    'id'              => true,
    'created_at'      => true,
    'updated_at'      => true,
    'source'          => true,
    'turns'           => true,
    'parent_id'       => true,
    'fork_point'      => true,
    'dispatched_from' => true,
    'injected_turns'  => true,
    'metadata'        => true,
];

/** @var array<string, true> */
const TURN_FIELDS = [
    'id'                  => true,
    'role'                => true,
    'created_at'          => true,
    'content'             => true,
    'agent_id'            => true,
    'in_reply_to_call_id' => true,
    'metadata'            => true,
];

/** @var array<string, true> */
const SOURCE_FIELDS = [
    'kind'     => true,
    'version'  => true,
    'instance' => true,
];

/** @var array<string, array<string, true>> */
const CONTENT_PART_FIELDS = [
    'text'        => ['type' => true, 'text' => true, 'metadata' => true],
    'tool_call'   => ['type' => true, 'call_id' => true, 'name' => true, 'input' => true, 'parent_call_id' => true, 'metadata' => true],
    'tool_result' => ['type' => true, 'call_id' => true, 'output' => true, 'is_error' => true, 'child_envelope_id' => true, 'metadata' => true],
    'image'       => ['type' => true, 'mime' => true, 'data' => true, 'url' => true, 'alt' => true, 'metadata' => true],
    'thinking'    => ['type' => true, 'text' => true, 'signature' => true, 'metadata' => true],
];

/**
 * Strictly decode a crtx Envelope from a JSON string.
 *
 * Rejects:
 *  - malformed JSON
 *  - unknown top-level Envelope fields
 *  - unknown ContentPart fields (per known type)
 *  - bad discriminators / missing required fields
 *  - unknown roles
 *  - image parts with both or neither of data/url
 *
 * Extension parts (`type` prefixed `x-`) round-trip verbatim.
 *
 * @throws EnvelopeParseException on any decode-time failure
 */
function parseEnvelope(string $json): Envelope
{
    try {
        /** @var mixed $decoded */
        $decoded = json_decode($json, true, 512, JSON_THROW_ON_ERROR);
        // Also decode in object mode so we can recover empty-object
        // intent on tool_result.output, which assoc-mode collapses to
        // []. We don't validate $objDecoded — assoc-mode is the source
        // of truth for shape checks; the object-mode tree is consulted
        // only at the tool_result.output position.
        // Second decode runs in object mode strictly to recover empty {}
        // vs [] distinctions that assoc mode collapses. PHP-only path;
        // sibling SDKs (go/ts/py/rs) preserve this natively via typed
        // decode. Cost is one extra pass over the same byte buffer.
        /** @var mixed $objDecoded */
        $objDecoded = json_decode($json, false, 512, JSON_THROW_ON_ERROR);
    } catch (JsonException $e) {
        throw new EnvelopeParseException('invalid json: ' . $e->getMessage(), 0, $e);
    }
    if (!is_array($decoded)) {
        throw new EnvelopeParseException('envelope must be a JSON object');
    }

    // Reject unknown top-level fields.
    foreach ($decoded as $k => $_) {
        if (!isset(ENVELOPE_FIELDS[(string) $k])) {
            throw new EnvelopeParseException(sprintf('unknown envelope field %s', (string) $k));
        }
    }

    foreach (['crtx_version', 'id', 'created_at', 'updated_at', 'source', 'turns'] as $required) {
        if (!array_key_exists($required, $decoded)) {
            throw new EnvelopeParseException(sprintf('missing required field %s', $required));
        }
    }

    if (!is_string($decoded['crtx_version'])) {
        throw new EnvelopeParseException('crtx_version must be a string');
    }
    if (!is_string($decoded['id'])) {
        throw new EnvelopeParseException('id must be a string');
    }
    if (!is_string($decoded['created_at'])) {
        throw new EnvelopeParseException('created_at must be a string');
    }
    if (!is_string($decoded['updated_at'])) {
        throw new EnvelopeParseException('updated_at must be a string');
    }
    if (!is_array($decoded['source'])) {
        throw new EnvelopeParseException('source must be an object');
    }
    foreach ($decoded['source'] as $k => $_) {
        if (!isset(SOURCE_FIELDS[(string) $k])) {
            throw new EnvelopeParseException(sprintf('unknown source field %s', (string) $k));
        }
    }
    if (!is_array($decoded['turns'])) {
        throw new EnvelopeParseException('turns must be an array');
    }

    $parentId  = $decoded['parent_id']  ?? null;
    $forkPoint = $decoded['fork_point'] ?? null;
    if ($parentId !== null && !is_string($parentId)) {
        throw new EnvelopeParseException('parent_id must be a string when present');
    }
    if ($forkPoint !== null && !is_int($forkPoint)) {
        throw new EnvelopeParseException('fork_point must be an integer when present');
    }
    if (is_int($forkPoint) && $forkPoint < 0) {
        // crtx v0.1 ranges fork_point >= 0; previously only validate()
        // caught negatives. Reject at parse so parseEnvelope refuses
        // negatives directly.
        throw new EnvelopeParseException('fork_point must be >= 0');
    }

    $dispatchedFrom = null;
    if (array_key_exists('dispatched_from', $decoded) && $decoded['dispatched_from'] !== null) {
        $df = $decoded['dispatched_from'];
        if (!is_array($df)) {
            throw new EnvelopeParseException('dispatched_from must be an object');
        }
        foreach ($df as $k => $_) {
            if ($k !== 'envelope_id' && $k !== 'call_id') {
                throw new EnvelopeParseException(sprintf('unknown dispatched_from field %s', (string) $k));
            }
        }
        $dfEnvId = $df['envelope_id'] ?? null;
        $dfCall  = $df['call_id']     ?? null;
        if (!is_string($dfEnvId) || $dfEnvId === '') {
            throw new EnvelopeParseException('dispatched_from.envelope_id must be a non-empty string');
        }
        if (!is_string($dfCall) || $dfCall === '') {
            throw new EnvelopeParseException('dispatched_from.call_id must be a non-empty string');
        }
        $dispatchedFrom = new DispatchedFrom($dfEnvId, $dfCall);
    }

    $injectedTurns = [];
    if (array_key_exists('injected_turns', $decoded) && $decoded['injected_turns'] !== null) {
        $raw = $decoded['injected_turns'];
        if (!is_array($raw) || ($raw !== [] && !array_is_list($raw))) {
            throw new EnvelopeParseException('injected_turns must be an array');
        }
        foreach ($raw as $i => $entry) {
            if (!is_array($entry)) {
                throw new EnvelopeParseException(sprintf('injected_turns[%d] must be an object', $i));
            }
            foreach ($entry as $k => $_) {
                if (!in_array($k, ['envelope_id', 'start_turn_id', 'end_turn_id', 'injected_after_turn_id'], true)) {
                    throw new EnvelopeParseException(sprintf('injected_turns[%d]: unknown field %s', $i, (string) $k));
                }
            }
            foreach (['envelope_id', 'start_turn_id', 'end_turn_id'] as $req) {
                if (!array_key_exists($req, $entry)) {
                    throw new EnvelopeParseException(sprintf('injected_turns[%d]: missing required field %s', $i, $req));
                }
                if (!is_string($entry[$req]) || $entry[$req] === '') {
                    throw new EnvelopeParseException(sprintf('injected_turns[%d].%s must be a non-empty string', $i, $req));
                }
            }
            $injectedAfter = $entry['injected_after_turn_id'] ?? null;
            if ($injectedAfter !== null && (!is_string($injectedAfter) || $injectedAfter === '')) {
                throw new EnvelopeParseException(sprintf('injected_turns[%d].injected_after_turn_id must be a non-empty string when present', $i));
            }
            $injectedTurns[] = new InjectedTurnRef(
                envelopeId: $entry['envelope_id'],
                startTurnId: $entry['start_turn_id'],
                endTurnId: $entry['end_turn_id'],
                injectedAfterTurnId: is_string($injectedAfter) ? $injectedAfter : null,
            );
        }
    }

    $metadata = $decoded['metadata'] ?? [];
    if (!is_array($metadata)) {
        throw new EnvelopeParseException('metadata must be an object');
    }
    // is_array() accepts numeric-indexed lists like [1,2,3]; crtx
    // marks metadata as `object`. Reject list-shaped metadata.
    if ($metadata !== [] && array_is_list($metadata)) {
        throw new EnvelopeParseException('metadata must be an object, not a list');
    }

    $source = Source::fromArray($decoded['source']);

    // Object-mode counterpart of $decoded['turns'], used solely to
    // disambiguate tool_result.output empty-{} vs empty-[] (PHP's
    // assoc-mode decode collapses both to []). Always resolved into a
    // list<mixed>; non-array shapes are caught by the assoc-mode walk
    // a few lines down.
    $objTurns = [];
    if (is_object($objDecoded) && isset($objDecoded->turns) && is_array($objDecoded->turns)) {
        $objTurns = array_values($objDecoded->turns);
    }

    $turns = [];
    foreach (array_values($decoded['turns']) as $i => $rawTurn) {
        if (!is_array($rawTurn)) {
            throw new EnvelopeParseException(sprintf('turns[%d] must be an object', $i));
        }
        $turns[] = decodeTurn($rawTurn, $objTurns[$i] ?? null, sprintf('turns[%d]', $i));
    }

    return new Envelope(
        crtxVersion: $decoded['crtx_version'],
        id: $decoded['id'],
        createdAt: $decoded['created_at'],
        updatedAt: $decoded['updated_at'],
        source: $source,
        turns: $turns,
        parentId: is_string($parentId) && $parentId !== '' ? $parentId : null,
        forkPoint: is_int($forkPoint) ? $forkPoint : null,
        metadata: $metadata,
        dispatchedFrom: $dispatchedFrom,
        injectedTurns: $injectedTurns,
    );
}

/**
 * Decode one Turn object. Internal helper for parseEnvelope.
 *
 * $objRaw is the object-mode counterpart of $raw (or null if not
 * available). It is consulted solely to disambiguate the empty-{}
 * vs empty-[] case on tool_result.output, since PHP's assoc-mode
 * decode collapses both to [].
 *
 * @param array<string, mixed> $raw
 *
 * @throws EnvelopeParseException
 */
function decodeTurn(array $raw, mixed $objRaw, string $path): Turn
{
    foreach ($raw as $k => $_) {
        if (!isset(TURN_FIELDS[(string) $k])) {
            throw new EnvelopeParseException(sprintf('%s: unknown turn field %s', $path, (string) $k));
        }
    }
    foreach (['id', 'role', 'created_at', 'content'] as $required) {
        if (!array_key_exists($required, $raw)) {
            throw new EnvelopeParseException(sprintf('%s: missing required field %s', $path, $required));
        }
    }
    if (!is_string($raw['id'])) {
        throw new EnvelopeParseException(sprintf('%s.id must be a string', $path));
    }
    if (!is_string($raw['role'])) {
        throw new EnvelopeParseException(sprintf('%s.role must be a string', $path));
    }
    if (!is_string($raw['created_at'])) {
        throw new EnvelopeParseException(sprintf('%s.created_at must be a string', $path));
    }
    if (!is_array($raw['content'])) {
        throw new EnvelopeParseException(sprintf('%s.content must be an array', $path));
    }
    // crtx v0.1 mandates content minItems=1; previously only validate()
    // caught this. Reject at parse so parseEnvelope() refuses empty
    // content[] turns directly, matching the other parse-time checks.
    if ($raw['content'] === []) {
        throw new EnvelopeParseException(sprintf('%s.content must have >=1 part', $path));
    }

    $role = RoleEnum::tryFrom($raw['role']);
    if ($role === null) {
        throw new EnvelopeParseException(sprintf('%s.role: unknown role %s', $path, $raw['role']));
    }

    $agentId = $raw['agent_id'] ?? null;
    if ($agentId !== null && (!is_string($agentId) || $agentId === '')) {
        throw new EnvelopeParseException(sprintf('%s.agent_id must be a non-empty string when present', $path));
    }
    $inReplyTo = $raw['in_reply_to_call_id'] ?? null;
    if ($inReplyTo !== null && (!is_string($inReplyTo) || $inReplyTo === '')) {
        throw new EnvelopeParseException(sprintf('%s.in_reply_to_call_id must be a non-empty string when present', $path));
    }

    $metadata = $raw['metadata'] ?? [];
    if (!is_array($metadata)) {
        throw new EnvelopeParseException(sprintf('%s.metadata must be an object', $path));
    }
    if ($metadata !== [] && array_is_list($metadata)) {
        throw new EnvelopeParseException(sprintf('%s.metadata must be an object, not a list', $path));
    }

    // Pull the matching content[] from the object-mode counterpart (when
    // present) so decodeContentPart can recover empty-{} intent on
    // tool_result.output.
    $objContent = [];
    if (is_object($objRaw) && isset($objRaw->content) && is_array($objRaw->content)) {
        $objContent = array_values($objRaw->content);
    }

    $content = [];
    foreach (array_values($raw['content']) as $i => $rawPart) {
        if (!is_array($rawPart)) {
            throw new EnvelopeParseException(sprintf('%s.content[%d] must be an object', $path, $i));
        }
        $content[] = decodeContentPart($rawPart, $objContent[$i] ?? null, sprintf('%s.content[%d]', $path, $i));
    }

    return new Turn(
        id: $raw['id'],
        role: $role,
        createdAt: $raw['created_at'],
        content: $content,
        metadata: $metadata,
        agentId: is_string($agentId) ? $agentId : null,
        inReplyToCallId: is_string($inReplyTo) ? $inReplyTo : null,
    );
}

/**
 * Decode one ContentPart. Discriminator-driven; extension parts
 * preserved verbatim via {@see ExtensionPart}.
 *
 * $objRaw is the object-mode counterpart of $raw (or null if not
 * available). Used solely to disambiguate empty-{} vs empty-[] on
 * tool_result.output.
 *
 * @param array<string, mixed> $raw
 *
 * @throws EnvelopeParseException
 */
function decodeContentPart(array $raw, mixed $objRaw, string $path): ContentPart
{
    $type = $raw['type'] ?? null;
    if (!is_string($type) || $type === '') {
        throw new EnvelopeParseException(sprintf('%s: missing or non-string type', $path));
    }

    // Extension part: must match ^x-[a-zA-Z0-9._-]+$ — preserve verbatim.
    if (str_starts_with($type, 'x-')) {
        if (!isValidExtensionType($type)) {
            throw new EnvelopeParseException(
                sprintf('%s: extension type %s does not match ^x-[a-zA-Z0-9._-]+$', $path, $type)
            );
        }

        return new ExtensionPart($type, $raw);
    }

    // Reject unknown ContentPart fields for known variants.
    if (!isset(CONTENT_PART_FIELDS[$type])) {
        throw new EnvelopeParseException(
            sprintf('%s: unknown ContentPart type %s (extension parts MUST use x- prefix)', $path, $type)
        );
    }
    $allowed = CONTENT_PART_FIELDS[$type];
    foreach ($raw as $k => $_) {
        if (!isset($allowed[(string) $k])) {
            throw new EnvelopeParseException(
                sprintf('%s: unknown field %s on %s part', $path, (string) $k, $type)
            );
        }
    }

    /** @var array<string, mixed> $metadata */
    $metadata = $raw['metadata'] ?? [];
    if (!is_array($metadata)) {
        throw new EnvelopeParseException(sprintf('%s.metadata must be an object', $path));
    }
    if ($metadata !== [] && array_is_list($metadata)) {
        throw new EnvelopeParseException(sprintf('%s.metadata must be an object, not a list', $path));
    }

    switch ($type) {
        case 'text':
            if (!array_key_exists('text', $raw) || !is_string($raw['text'])) {
                throw new EnvelopeParseException(sprintf('%s: text part missing required text:string', $path));
            }

            return new TextPart($raw['text'], $metadata);

        case 'tool_call':
            if (!array_key_exists('call_id', $raw) || !is_string($raw['call_id']) || $raw['call_id'] === '') {
                throw new EnvelopeParseException(sprintf('%s: tool_call missing non-empty call_id', $path));
            }
            if (!array_key_exists('name', $raw) || !is_string($raw['name']) || $raw['name'] === '') {
                throw new EnvelopeParseException(sprintf('%s: tool_call missing non-empty name', $path));
            }
            if (!array_key_exists('input', $raw)) {
                throw new EnvelopeParseException(sprintf('%s: tool_call missing input', $path));
            }
            $input = $raw['input'];
            if (!is_array($input) && !is_string($input)) {
                throw new EnvelopeParseException(
                    sprintf('%s: tool_call.input must be an object or string', $path)
                );
            }
            $parentCallId = $raw['parent_call_id'] ?? null;
            if ($parentCallId !== null && (!is_string($parentCallId) || $parentCallId === '')) {
                throw new EnvelopeParseException(
                    sprintf('%s: tool_call.parent_call_id must be a non-empty string when present', $path)
                );
            }

            return new ToolCallPart(
                callId: $raw['call_id'],
                name: $raw['name'],
                input: $input,
                metadata: $metadata,
                parentCallId: is_string($parentCallId) ? $parentCallId : null,
            );

        case 'tool_result':
            if (!array_key_exists('call_id', $raw) || !is_string($raw['call_id']) || $raw['call_id'] === '') {
                throw new EnvelopeParseException(sprintf('%s: tool_result missing non-empty call_id', $path));
            }
            if (!array_key_exists('output', $raw)) {
                throw new EnvelopeParseException(sprintf('%s: tool_result missing output', $path));
            }
            $isError = $raw['is_error'] ?? false;
            if (!is_bool($isError)) {
                throw new EnvelopeParseException(sprintf('%s: tool_result.is_error must be a boolean', $path));
            }
            $childEnvId = $raw['child_envelope_id'] ?? null;
            if ($childEnvId !== null && (!is_string($childEnvId) || $childEnvId === '')) {
                throw new EnvelopeParseException(
                    sprintf('%s: tool_result.child_envelope_id must be a non-empty string when present', $path)
                );
            }

            // PHP's json_decode($json, true) collapses incoming JSON {}
            // to []. The producer's empty-object intent is lost before
            // we ever see $raw. Recover it by walking the assoc tree in
            // lockstep with the object-mode counterpart: at every
            // position where assoc is [] AND obj is a stdClass, swap in
            // new stdClass(). Recurses into nested arrays/objects so
            // empty {} survives at any depth inside tool_result.output.
            $output = $raw['output'];
            if (is_object($objRaw) && property_exists($objRaw, 'output')) {
                $output = restoreEmptyObjects($output, $objRaw->output);
            }

            return new ToolResultPart(
                callId: $raw['call_id'],
                output: $output,
                isError: $isError,
                metadata: $metadata,
                childEnvelopeId: is_string($childEnvId) ? $childEnvId : null,
            );

        case 'image':
            if (!array_key_exists('mime', $raw) || !is_string($raw['mime']) || $raw['mime'] === '') {
                throw new EnvelopeParseException(sprintf('%s: image missing non-empty mime', $path));
            }
            $data = $raw['data'] ?? null;
            $url  = $raw['url']  ?? null;
            if ($data !== null && !is_string($data)) {
                throw new EnvelopeParseException(sprintf('%s: image.data must be a string', $path));
            }
            if ($url !== null && !is_string($url)) {
                throw new EnvelopeParseException(sprintf('%s: image.url must be a string', $path));
            }
            $alt = $raw['alt'] ?? null;
            if ($alt !== null && !is_string($alt)) {
                throw new EnvelopeParseException(sprintf('%s: image.alt must be a string', $path));
            }

            // ImagePart constructor enforces XOR.
            return new ImagePart($raw['mime'], $data, $url, $alt, $metadata);

        case 'thinking':
            if (!array_key_exists('text', $raw) || !is_string($raw['text'])) {
                throw new EnvelopeParseException(sprintf('%s: thinking missing text:string', $path));
            }
            $sig = $raw['signature'] ?? null;
            if ($sig !== null && !is_string($sig)) {
                throw new EnvelopeParseException(sprintf('%s: thinking.signature must be a string', $path));
            }

            return new ThinkingPart($raw['text'], $sig, $metadata);

        default:
            // Unreachable: guarded by CONTENT_PART_FIELDS membership check above.
            throw new EnvelopeParseException(sprintf('%s: unhandled ContentPart type %s', $path, $type));
    }
}

/**
 * Walk an assoc-mode value in lockstep with its object-mode counterpart
 * and restore empty-{} intent that PHP's assoc decode collapsed to [].
 *
 * Used by decodeContentPart on tool_result.output. The assoc tree is
 * the source of truth for shape; the object tree is consulted ONLY at
 * positions where the assoc value is [] (ambiguous: empty list or
 * empty object?) — when the counterpart at that position is a
 * stdClass, the source was {} and we substitute new stdClass() so
 * json_encode emits {}.
 *
 * Recurses into nested arrays/objects so empty {} survives at any
 * depth inside output. When the object-mode counterpart is missing
 * (null / property absent) at a position, the assoc value is returned
 * verbatim — we only fix what we can prove was {} in the source.
 *
 * Cases:
 *   1. assoc===[] + obj is stdClass        -> new stdClass() (the fix)
 *   2. assoc is list array + obj is array  -> recurse positionally
 *   3. assoc is map array + obj is stdClass-> recurse per key
 *   4. anything else                       -> return assoc untouched
 */
function restoreEmptyObjects(mixed $assoc, mixed $obj): mixed
{
    // Case 1: empty assoc array + stdClass counterpart -> empty object
    // Catches both top-level "output":{} and any nested "k":{}.
    if (is_array($assoc) && $assoc === [] && $obj instanceof \stdClass) {
        return new \stdClass();
    }

    // No counterpart to consult, or assoc is a scalar/null: nothing to
    // recover. Return the assoc value as-is.
    if (!is_array($assoc)) {
        return $assoc;
    }

    // Lists: recurse positionally when obj is also a list. If obj isn't
    // a list (mismatch — shouldn't happen for well-formed JSON, since
    // both decodes saw the same byte stream), fall through to the assoc
    // value untouched.
    if (array_is_list($assoc)) {
        if (!is_array($obj) || !array_is_list($obj)) {
            return $assoc;
        }
        $out = [];
        foreach ($assoc as $i => $v) {
            $out[] = restoreEmptyObjects($v, $obj[$i] ?? null);
        }

        return $out;
    }

    // Maps (string-keyed assoc array): recurse per key into the stdClass
    // counterpart. If obj isn't a stdClass, leave assoc untouched.
    if (!$obj instanceof \stdClass) {
        return $assoc;
    }
    $out = [];
    foreach ($assoc as $k => $v) {
        $key      = (string) $k;
        $childObj = property_exists($obj, $key) ? $obj->{$key} : null;
        $out[$k]  = restoreEmptyObjects($v, $childObj);
    }

    return $out;
}

/**
 * Validate ^x-[a-zA-Z0-9._-]+$ — requires >=1 char past the prefix and
 * restricts the char class.
 */
function isValidExtensionType(string $s): bool
{
    return (bool) preg_match('/^x-[a-zA-Z0-9._\-]+$/', $s);
}

/**
 * Validate ^[a-z]+/[a-zA-Z0-9.+-]+$ — crtx v0.1 image.mime regex.
 */
function isValidMimeType(string $s): bool
{
    return (bool) preg_match('/^[a-z]+\/[a-zA-Z0-9.+\-]+$/', $s);
}

/**
 * Serialize an Envelope to a crtx-canonical JSON string.
 *
 * Empty turns serialize to `[]`, never `null`. Extension parts round-trip
 * verbatim. ImagePart with both-or-neither data/url is impossible because
 * the value type enforces XOR in its constructor.
 *
 * @throws EnvelopeParseException on JSON encoding failure (which would
 *                                indicate a non-serializable value in
 *                                metadata or a tool result).
 */
function serializeEnvelope(Envelope $env): string
{
    try {
        $json = json_encode($env, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
    } catch (JsonException $e) {
        throw new EnvelopeParseException('serialize failed: ' . $e->getMessage(), 0, $e);
    }
    /** @var string $json — JSON_THROW_ON_ERROR ensures non-false. */

    return $json;
}

/**
 * Structural validation of an in-memory Envelope.
 *
 * Mirrors hop.top/stem (Go) Validate(): required fields, role enum,
 * ContentPart discriminator, parent_id ↔ fork_point dependency,
 * tool_result.call_id → preceding tool_call.call_id linkage.
 *
 * Unlike Validate in Go (which returns the first error), this function
 * returns ALL errors so callers can surface a complete report.
 *
 * @return list<EnvelopeError>
 */
function validate(Envelope $env): array
{
    $errors = [];

    if ($env->crtxVersion === '') {
        $errors[] = new EnvelopeError('crtx_version', 'missing');
    } elseif (!crtxVersionCompatible($env->crtxVersion)) {
        $errors[] = new EnvelopeError(
            'crtx_version',
            sprintf('%s unsupported (this stem speaks %s)', $env->crtxVersion, Stem::CRTX_VERSION),
        );
    }
    if ($env->id === '') {
        $errors[] = new EnvelopeError('id', 'missing');
    }
    if ($env->createdAt === '') {
        $errors[] = new EnvelopeError('created_at', 'missing');
    }
    if ($env->updatedAt === '') {
        $errors[] = new EnvelopeError('updated_at', 'missing');
    }
    if ($env->source->kind === '' || $env->source->version === '') {
        $errors[] = new EnvelopeError('source', 'kind and version required');
    }

    // parent_id ↔ fork_point: both or neither.
    $hasParent = $env->parentId !== null && $env->parentId !== '';
    $hasFork   = $env->forkPoint !== null;
    if ($hasParent !== $hasFork) {
        $errors[] = new EnvelopeError(
            '',
            'parent_id and fork_point must both be set or both omitted',
        );
    }
    if ($env->forkPoint !== null && $env->forkPoint < 0) {
        $errors[] = new EnvelopeError('fork_point', 'must be >= 0');
    }

    // dispatched_from is mutually exclusive with parent_id / fork_point
    // (envelope.md §7.2). Both members are required by the schema.
    if ($env->dispatchedFrom !== null) {
        if ($hasParent || $hasFork) {
            $errors[] = new EnvelopeError(
                'dispatched_from',
                'mutually exclusive with parent_id/fork_point',
            );
        }
        if ($env->dispatchedFrom->envelopeId === '' || $env->dispatchedFrom->callId === '') {
            $errors[] = new EnvelopeError(
                'dispatched_from',
                'requires both envelope_id and call_id',
            );
        }
    }

    // injected_turns: required tuple, plus injected_after_turn_id (when
    // set) MUST reference a turn already in this envelope's turns[]
    // (envelope.md §7.3 — forward refs forbidden).
    $envelopeTurnIds = [];
    foreach ($env->turns as $t) {
        $envelopeTurnIds[$t->id] = true;
    }
    foreach ($env->injectedTurns as $i => $ref) {
        if ($ref->envelopeId === '' || $ref->startTurnId === '' || $ref->endTurnId === '') {
            $errors[] = new EnvelopeError(
                sprintf('injected_turns[%d]', $i),
                'envelope_id / start_turn_id / end_turn_id required',
            );
        }
        if ($ref->injectedAfterTurnId !== null && $ref->injectedAfterTurnId !== '') {
            if (!isset($envelopeTurnIds[$ref->injectedAfterTurnId])) {
                $errors[] = new EnvelopeError(
                    sprintf('injected_turns[%d]', $i),
                    sprintf(
                        'injected_after_turn_id %s not present in this envelope',
                        $ref->injectedAfterTurnId,
                    ),
                );
            }
        }
    }

    // Track open tool_calls. A call goes open on tool_call and closes
    // on its matching tool_result. Used by:
    //   - tool_result.call_id linkage
    //   - tool_call.parent_call_id (MUST point at an open outer call)
    //   - Turn.in_reply_to_call_id (MUST point at an open call)
    /** @var array<string, true> $openCalls */
    $openCalls = [];
    foreach ($env->turns as $i => $turn) {
        validateTurn($turn, $openCalls, sprintf('turns[%d]', $i), $errors);
    }

    return $errors;
}

/**
 * Strict-decode + structural validate.
 *
 * Catches both unknown-field drift (parseEnvelope's strict decode) and
 * the relational/semantic checks that {@see validate()} layers on top.
 *
 * @return list<EnvelopeError>
 *
 * @throws EnvelopeParseException on decode-time failure (unknown fields,
 *                                bad JSON, malformed extensions).
 */
function validateBytes(string $json): array
{
    return validate(parseEnvelope($json));
}

/**
 * @param array<string, true>   $openCalls
 * @param list<EnvelopeError>   $errors
 */
function validateTurn(Turn $turn, array &$openCalls, string $path, array &$errors): void
{
    if ($turn->id === '') {
        $errors[] = new EnvelopeError($path . '.id', 'missing');
    }
    if ($turn->createdAt === '') {
        $errors[] = new EnvelopeError($path . '.created_at', 'missing');
    }

    // in_reply_to_call_id (envelope.md §3.2) MUST reference an open
    // tool_call at the point this Turn is appended. Tool-role Turns
    // SHOULD NOT carry it — tool_result IS the resolution of the call,
    // not an interjection.
    if ($turn->inReplyToCallId !== null && $turn->inReplyToCallId !== '') {
        if ($turn->role === RoleEnum::Tool) {
            $errors[] = new EnvelopeError(
                $path,
                'in_reply_to_call_id: forbidden on tool-role turns',
            );
        } elseif (!isset($openCalls[$turn->inReplyToCallId])) {
            $errors[] = new EnvelopeError(
                $path,
                sprintf(
                    'in_reply_to_call_id %s has no open tool_call',
                    $turn->inReplyToCallId,
                ),
            );
        }
    }

    if ($turn->content === []) {
        $errors[] = new EnvelopeError($path . '.content', 'must have >=1 part');

        return;
    }
    foreach ($turn->content as $j => $part) {
        validateContentPart($part, $openCalls, sprintf('%s.content[%d]', $path, $j), $errors);
    }
}

/**
 * @param array<string, true>   $openCalls
 * @param list<EnvelopeError>   $errors
 */
function validateContentPart(
    ContentPart $part,
    array &$openCalls,
    string $path,
    array &$errors,
): void {
    if ($part->type() === '') {
        $errors[] = new EnvelopeError($path, 'missing type');

        return;
    }
    if ($part instanceof ExtensionPart) {
        if (!isValidExtensionType($part->type())) {
            $errors[] = new EnvelopeError(
                $path,
                sprintf('extension type %s does not match ^x-[a-zA-Z0-9._-]+$', $part->type()),
            );
        }

        return;
    }
    if ($part instanceof TextPart) {
        // Empty string permitted by schema; only `type` + `text` required.
        return;
    }
    if ($part instanceof ToolCallPart) {
        if ($part->callId === '') {
            $errors[] = new EnvelopeError($path, 'tool_call: missing call_id');
        }
        if ($part->name === '') {
            $errors[] = new EnvelopeError($path, 'tool_call: missing name');
        }
        // parent_call_id (envelope.md §6.2) MUST reference an outer
        // tool_call still open at this position.
        if ($part->parentCallId !== null && $part->parentCallId !== '') {
            if (!isset($openCalls[$part->parentCallId])) {
                $errors[] = new EnvelopeError(
                    $path,
                    sprintf(
                        'tool_call: parent_call_id %s has no open outer tool_call',
                        $part->parentCallId,
                    ),
                );
            }
        }
        if ($part->callId !== '') {
            $openCalls[$part->callId] = true;
        }

        return;
    }
    if ($part instanceof ToolResultPart) {
        if ($part->callId === '') {
            $errors[] = new EnvelopeError($path, 'tool_result: missing call_id');

            return;
        }
        if (!isset($openCalls[$part->callId])) {
            $errors[] = new EnvelopeError(
                $path,
                sprintf('tool_result: call_id %s has no preceding open tool_call', $part->callId),
            );

            return;
        }
        // Close the matched call so later turns can't interject into
        // it via in_reply_to_call_id, and nested calls can't claim it
        // as a parent.
        unset($openCalls[$part->callId]);

        return;
    }
    if ($part instanceof ImagePart) {
        if ($part->mime === '') {
            $errors[] = new EnvelopeError($path, 'image: missing mime');
        } elseif (!isValidMimeType($part->mime)) {
            $errors[] = new EnvelopeError(
                $path,
                sprintf('image: mime %s does not match ^[a-z]+/[a-zA-Z0-9.+-]+$', $part->mime),
            );
        }
        // XOR enforced at construction; nothing further here.
        return;
    }
    if ($part instanceof ThinkingPart) {
        // Empty thinking.text permitted by schema; only the key itself
        // is required (value may be ""). Mirrors text part rules.
        return;
    }

    // Custom user-defined ContentPart implementation; we don't know the
    // shape, so we cannot validate beyond the discriminator.
    $errors[] = new EnvelopeError(
        $path,
        sprintf('unrecognized ContentPart implementation: %s', $part::class),
    );
}

/**
 * Match the producing-version policy from Go: within 0.x we only accept
 * the exact version we speak.
 */
function crtxVersionCompatible(string $v): bool
{
    return $v === Stem::CRTX_VERSION;
}
