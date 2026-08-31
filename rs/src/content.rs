//! `Role` enum + `ContentPart` discriminated union for crtx v0.1.

use serde::{Deserialize, Serialize};

/// Role identifies the author of a Turn. Mirrors the crtx v0.1 enum.
///
/// Unknown role strings fail decode with a serde error — crtx spec §4
/// requires implementations to reject the envelope rather than coerce
/// silently to a catch-all variant.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Role {
    User,
    Assistant,
    Tool,
    System,
    Developer,
}

impl Role {
    pub fn as_str(&self) -> &'static str {
        match self {
            Role::User => "user",
            Role::Assistant => "assistant",
            Role::Tool => "tool",
            Role::System => "system",
            Role::Developer => "developer",
        }
    }
}

/// A single payload unit inside a [`crate::Turn`].
///
/// `ContentPart` is a discriminated union on `type`. v0.1 variants:
/// `Text`, `ToolCall`, `ToolResult`, `Image`, `Thinking`. Anything
/// else round-trips verbatim through [`ContentPart::Extension`] —
/// producers MUST prefix the extension type with `x-` per spec.
///
/// Serialization is hand-rolled (see the `impl Serialize` /
/// `impl Deserialize` below) so the known variants enforce
/// `deny_unknown_fields` while extension parts round-trip with
/// arbitrary extra fields preserved.
#[derive(Debug, Clone, PartialEq)]
pub enum ContentPart {
    Text {
        text: String,
        metadata: Option<serde_json::Value>,
    },
    ToolCall {
        call_id: String,
        name: String,
        input: serde_json::Value,
        /// References an outer `tool_call.call_id` still open at this
        /// position when this call was issued as nested dispatch. See
        /// envelope.md §6.2.
        parent_call_id: Option<String>,
        metadata: Option<serde_json::Value>,
    },
    ToolResult {
        call_id: String,
        output: serde_json::Value,
        is_error: bool,
        /// Points at the dispatch-nest child Envelope's `id` when the
        /// call was executed out-of-band as a fresh session. See
        /// envelope.md §7.2.
        child_envelope_id: Option<String>,
        metadata: Option<serde_json::Value>,
    },
    Image {
        mime: String,
        data: Option<String>,
        url: Option<String>,
        alt: Option<String>,
        metadata: Option<serde_json::Value>,
    },
    Thinking {
        text: String,
        signature: Option<String>,
        metadata: Option<serde_json::Value>,
    },
    /// Catch-all for `x-*` extension parts. The full original JSON
    /// object is preserved verbatim under `raw`; the discriminator
    /// (`type`) is captured in `type_name`.
    Extension {
        type_name: String,
        raw: serde_json::Value,
    },
}

/// Convenience constructor for [`ContentPart::Text`].
impl ContentPart {
    pub fn text(text: impl Into<String>) -> Self {
        Self::Text {
            text: text.into(),
            metadata: None,
        }
    }

    pub fn tool_call(
        call_id: impl Into<String>,
        name: impl Into<String>,
        input: serde_json::Value,
    ) -> Self {
        Self::ToolCall {
            call_id: call_id.into(),
            name: name.into(),
            input,
            parent_call_id: None,
            metadata: None,
        }
    }

    /// Constructs a `tool_call` with `parent_call_id` set, marking it
    /// as nested dispatch within an outer open call. See envelope.md
    /// §6.2.
    pub fn tool_call_nested(
        call_id: impl Into<String>,
        name: impl Into<String>,
        input: serde_json::Value,
        parent_call_id: impl Into<String>,
    ) -> Self {
        Self::ToolCall {
            call_id: call_id.into(),
            name: name.into(),
            input,
            parent_call_id: Some(parent_call_id.into()),
            metadata: None,
        }
    }

    pub fn tool_result(
        call_id: impl Into<String>,
        output: serde_json::Value,
        is_error: bool,
    ) -> Self {
        Self::ToolResult {
            call_id: call_id.into(),
            output,
            is_error,
            child_envelope_id: None,
            metadata: None,
        }
    }

    /// Constructs a `tool_result` whose execution ran out-of-band as a
    /// dispatch-nest child Envelope; `child_envelope_id` lets consumers
    /// walk to the full child transcript. See envelope.md §7.2.
    pub fn tool_result_with_child(
        call_id: impl Into<String>,
        output: serde_json::Value,
        is_error: bool,
        child_envelope_id: impl Into<String>,
    ) -> Self {
        Self::ToolResult {
            call_id: call_id.into(),
            output,
            is_error,
            child_envelope_id: Some(child_envelope_id.into()),
            metadata: None,
        }
    }

    /// Discriminator string as it would appear on the wire.
    pub fn type_name(&self) -> &str {
        match self {
            Self::Text { .. } => "text",
            Self::ToolCall { .. } => "tool_call",
            Self::ToolResult { .. } => "tool_result",
            Self::Image { .. } => "image",
            Self::Thinking { .. } => "thinking",
            Self::Extension { type_name, .. } => type_name,
        }
    }

    /// True for `x-*` extension parts.
    pub fn is_extension(&self) -> bool {
        matches!(self, Self::Extension { .. })
    }
}

// --- Custom (de)serialization to honour deny_unknown_fields on known
// variants while letting x-* extensions round-trip with arbitrary
// extra fields. The default `#[serde(tag = "type")]` derive cannot
// express both simultaneously; the hand-rolled impls below split the
// known variants (strict) from the extension catchall (verbatim).

impl Serialize for ContentPart {
    fn serialize<S: serde::Serializer>(&self, ser: S) -> Result<S::Ok, S::Error> {
        use serde::ser::SerializeMap;

        match self {
            Self::Text { text, metadata } => {
                let mut map = ser.serialize_map(None)?;
                map.serialize_entry("type", "text")?;
                map.serialize_entry("text", text)?;
                if let Some(m) = metadata {
                    map.serialize_entry("metadata", m)?;
                }
                map.end()
            }
            Self::ToolCall {
                call_id,
                name,
                input,
                parent_call_id,
                metadata,
            } => {
                let mut map = ser.serialize_map(None)?;
                map.serialize_entry("type", "tool_call")?;
                map.serialize_entry("call_id", call_id)?;
                map.serialize_entry("name", name)?;
                map.serialize_entry("input", input)?;
                if let Some(p) = parent_call_id {
                    map.serialize_entry("parent_call_id", p)?;
                }
                if let Some(m) = metadata {
                    map.serialize_entry("metadata", m)?;
                }
                map.end()
            }
            Self::ToolResult {
                call_id,
                output,
                is_error,
                child_envelope_id,
                metadata,
            } => {
                let mut map = ser.serialize_map(None)?;
                map.serialize_entry("type", "tool_result")?;
                map.serialize_entry("call_id", call_id)?;
                map.serialize_entry("output", output)?;
                if *is_error {
                    map.serialize_entry("is_error", &true)?;
                }
                if let Some(c) = child_envelope_id {
                    map.serialize_entry("child_envelope_id", c)?;
                }
                if let Some(m) = metadata {
                    map.serialize_entry("metadata", m)?;
                }
                map.end()
            }
            Self::Image {
                mime,
                data,
                url,
                alt,
                metadata,
            } => {
                let mut map = ser.serialize_map(None)?;
                map.serialize_entry("type", "image")?;
                map.serialize_entry("mime", mime)?;
                let has_data = data.is_some();
                let has_url = url.is_some();
                if has_data == has_url {
                    return Err(serde::ser::Error::custom(
                        "image part: exactly one of data/url required",
                    ));
                }
                if let Some(d) = data {
                    map.serialize_entry("data", d)?;
                }
                if let Some(u) = url {
                    map.serialize_entry("url", u)?;
                }
                if let Some(a) = alt {
                    map.serialize_entry("alt", a)?;
                }
                if let Some(m) = metadata {
                    map.serialize_entry("metadata", m)?;
                }
                map.end()
            }
            Self::Thinking {
                text,
                signature,
                metadata,
            } => {
                let mut map = ser.serialize_map(None)?;
                map.serialize_entry("type", "thinking")?;
                map.serialize_entry("text", text)?;
                if let Some(s) = signature {
                    map.serialize_entry("signature", s)?;
                }
                if let Some(m) = metadata {
                    map.serialize_entry("metadata", m)?;
                }
                map.end()
            }
            Self::Extension { raw, .. } => raw.serialize(ser),
        }
    }
}

impl<'de> Deserialize<'de> for ContentPart {
    fn deserialize<D: serde::Deserializer<'de>>(de: D) -> Result<Self, D::Error> {
        // First decode into a generic Value so we can branch on the
        // discriminator, then apply strict per-variant decoding for
        // known types and verbatim capture for x-* extensions.
        let value = serde_json::Value::deserialize(de)?;
        let obj = value
            .as_object()
            .ok_or_else(|| serde::de::Error::custom("ContentPart: not a JSON object"))?;
        let type_name = obj
            .get("type")
            .and_then(|v| v.as_str())
            .ok_or_else(|| serde::de::Error::custom("ContentPart: missing type"))?
            .to_owned();

        if type_name.starts_with("x-") {
            return Ok(Self::Extension {
                type_name,
                raw: value,
            });
        }

        // Strict-decode known variants: build a typed struct per shape
        // and reject unknown fields.
        match type_name.as_str() {
            "text" => {
                let v: TextWire =
                    serde_json::from_value(value).map_err(serde::de::Error::custom)?;
                Ok(Self::Text {
                    text: v.text,
                    metadata: v.metadata,
                })
            }
            "tool_call" => {
                let v: ToolCallWire =
                    serde_json::from_value(value).map_err(serde::de::Error::custom)?;
                Ok(Self::ToolCall {
                    call_id: v.call_id,
                    name: v.name,
                    input: v.input,
                    parent_call_id: v.parent_call_id,
                    metadata: v.metadata,
                })
            }
            "tool_result" => {
                let v: ToolResultWire =
                    serde_json::from_value(value).map_err(serde::de::Error::custom)?;
                Ok(Self::ToolResult {
                    call_id: v.call_id,
                    output: v.output,
                    is_error: v.is_error.unwrap_or(false),
                    child_envelope_id: v.child_envelope_id,
                    metadata: v.metadata,
                })
            }
            "image" => {
                let v: ImageWire =
                    serde_json::from_value(value).map_err(serde::de::Error::custom)?;
                Ok(Self::Image {
                    mime: v.mime,
                    data: v.data,
                    url: v.url,
                    alt: v.alt,
                    metadata: v.metadata,
                })
            }
            "thinking" => {
                let v: ThinkingWire =
                    serde_json::from_value(value).map_err(serde::de::Error::custom)?;
                Ok(Self::Thinking {
                    text: v.text,
                    signature: v.signature,
                    metadata: v.metadata,
                })
            }
            other => Err(serde::de::Error::custom(format!(
                "ContentPart: unknown type {other:?} (extension parts MUST use x- prefix)"
            ))),
        }
    }
}

// --- Per-variant wire structs used purely for strict decoding. -------

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct TextWire {
    #[serde(rename = "type")]
    _type: TypeText,
    text: String,
    #[serde(default)]
    metadata: Option<serde_json::Value>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ToolCallWire {
    #[serde(rename = "type")]
    _type: TypeToolCall,
    call_id: String,
    name: String,
    input: serde_json::Value,
    #[serde(default)]
    parent_call_id: Option<String>,
    #[serde(default)]
    metadata: Option<serde_json::Value>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ToolResultWire {
    #[serde(rename = "type")]
    _type: TypeToolResult,
    call_id: String,
    output: serde_json::Value,
    #[serde(default)]
    is_error: Option<bool>,
    #[serde(default)]
    child_envelope_id: Option<String>,
    #[serde(default)]
    metadata: Option<serde_json::Value>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ImageWire {
    #[serde(rename = "type")]
    _type: TypeImage,
    mime: String,
    #[serde(default)]
    data: Option<String>,
    #[serde(default)]
    url: Option<String>,
    #[serde(default)]
    alt: Option<String>,
    #[serde(default)]
    metadata: Option<serde_json::Value>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ThinkingWire {
    #[serde(rename = "type")]
    _type: TypeThinking,
    text: String,
    #[serde(default)]
    signature: Option<String>,
    #[serde(default)]
    metadata: Option<serde_json::Value>,
}

// Marker enums whose only valid value is the matching discriminator
// string. Lets the per-variant struct above reject mis-typed parts.
macro_rules! type_marker {
    ($name:ident, $literal:literal) => {
        #[derive(Deserialize)]
        enum $name {
            #[serde(rename = $literal)]
            Tag,
        }
    };
}
type_marker!(TypeText, "text");
type_marker!(TypeToolCall, "tool_call");
type_marker!(TypeToolResult, "tool_result");
type_marker!(TypeImage, "image");
type_marker!(TypeThinking, "thinking");

/// Reports whether `s` matches the crtx v0.1 extension type regex
/// `^x-[a-zA-Z0-9._-]+$`. Public so [`crate::validate`] (and tests)
/// can apply the same rule.
pub fn is_valid_extension_type(s: &str) -> bool {
    if s.len() < 3 || !s.starts_with("x-") {
        return false;
    }
    s[2..]
        .chars()
        .all(|c| c.is_ascii_alphanumeric() || c == '.' || c == '_' || c == '-')
}

/// Reports whether `s` matches the crtx v0.1 image.mime regex
/// `^[a-z]+/[a-zA-Z0-9.+-]+$`. type token is lowercase letters only;
/// subtype permits alphanumerics plus `.`, `+`, `-`.
pub fn is_valid_mime_type(s: &str) -> bool {
    let Some(slash) = s.find('/') else {
        return false;
    };
    if slash == 0 || slash == s.len() - 1 {
        return false;
    }
    let (typ, rest) = s.split_at(slash);
    let sub = &rest[1..];
    if !typ.chars().all(|c| c.is_ascii_lowercase()) {
        return false;
    }
    sub.chars()
        .all(|c| c.is_ascii_alphanumeric() || c == '.' || c == '+' || c == '-')
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unknown_role_fails_serde_decode() {
        // Per crtx spec §4 implementations MUST reject unknown roles
        // rather than coerce. The `#[serde(other)]` catch-all that used
        // to live here was removed; surface the decode error instead.
        let json = r#""wizard""#;
        let res: Result<Role, _> = serde_json::from_str(json);
        assert!(res.is_err());
    }

    #[test]
    fn extension_type_regex() {
        assert!(is_valid_extension_type("x-io.jadb.foo"));
        assert!(is_valid_extension_type("x-a"));
        assert!(!is_valid_extension_type("x-"));
        assert!(!is_valid_extension_type("x"));
        assert!(!is_valid_extension_type("x-bad space"));
        assert!(!is_valid_extension_type("custom"));
    }
}
