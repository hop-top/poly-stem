<?php

declare(strict_types=1);

namespace HopTop\Stem;

use RuntimeException;

/**
 * Thrown by {@see parseEnvelope()} and {@see validateBytes()} when an
 * envelope cannot be parsed strictly:
 *
 *  - malformed JSON
 *  - missing required fields
 *  - unknown top-level or content-part fields
 *  - bad discriminator on a ContentPart
 *
 * Validate() collects structural errors into an EnvelopeError[] and
 * returns them; parseEnvelope throws this exception on the first
 * decode-time failure.
 */
final class EnvelopeParseException extends RuntimeException
{
}
