<?php

declare(strict_types=1);

/**
 * parity-roundtrip — read a crtx v0.1 envelope JSON file at argv[1],
 * parse via HopTop\Stem\parseEnvelope, re-serialize via
 * HopTop\Stem\serializeEnvelope, write the result to stdout.
 *
 * Used by tools/parity/runner.sh. Run from php/ as:
 *
 *   php tools/parity-roundtrip.php <path>
 */

require __DIR__ . '/../vendor/autoload.php';

use function HopTop\Stem\parseEnvelope;
use function HopTop\Stem\serializeEnvelope;

if ($argc !== 2) {
    fwrite(STDERR, "usage: parity-roundtrip <envelope.json>\n");
    exit(2);
}

$path = $argv[1];
$text = @file_get_contents($path);
if ($text === false) {
    fwrite(STDERR, sprintf("parity-roundtrip: read %s: %s\n", $path, error_get_last()['message'] ?? 'unknown error'));
    exit(1);
}

try {
    $env = parseEnvelope($text);
} catch (\Throwable $e) {
    fwrite(STDERR, sprintf("parity-roundtrip: parse: %s\n", $e->getMessage()));
    exit(1);
}

try {
    $out = serializeEnvelope($env);
} catch (\Throwable $e) {
    fwrite(STDERR, sprintf("parity-roundtrip: serialize: %s\n", $e->getMessage()));
    exit(1);
}

fwrite(STDOUT, $out);
