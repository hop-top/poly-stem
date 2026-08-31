"""Stem SDK version constants.

CRTX_VERSION is the crtx spec version emitted on every Envelope.
VERSION is the stem semver, surfaced as Source.version on stem-produced
Envelopes. SOURCE_KIND identifies stem as the producing runtime.
"""
from __future__ import annotations

CRTX_VERSION: str = "0.1"
VERSION: str = "0.1.0"
SOURCE_KIND: str = "stem"
