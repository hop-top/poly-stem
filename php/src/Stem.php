<?php

declare(strict_types=1);

namespace HopTop\Stem;

/**
 * Package-level constants for the stem PHP SDK.
 *
 * Mirrors hop.top/stem (Go) version.go.
 */
final class Stem
{
    /** crtx spec version emitted on every Envelope. */
    public const CRTX_VERSION = '0.1';

    /** stem semver, surfaced as source.version on stem-produced envelopes. */
    public const VERSION = '0.1.0';

    /** kind identifier for stem as a producer. */
    public const SOURCE_KIND = 'stem';

    private function __construct()
    {
    }
}
