<?php

declare(strict_types=1);

namespace HopTop\Stem;

/**
 * Producer of a Turn. Follows crtx v0.1 enum.
 *
 * String-backed so json_encode/json_decode round-trip the wire form.
 */
enum RoleEnum: string
{
    case User      = 'user';
    case Assistant = 'assistant';
    case Tool      = 'tool';
    case System    = 'system';
    case Developer = 'developer';
}
