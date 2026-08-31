package stem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Built-in ContentPart type values from the crtx v0.1 enum.
const (
	PartTypeText       = "text"
	PartTypeToolCall   = "tool_call"
	PartTypeToolResult = "tool_result"
	PartTypeImage      = "image"
	PartTypeThinking   = "thinking"
)

// ContentPart is a discriminated union on Type. v0.1 variants:
// text, tool_call, tool_result, image, thinking. Extension parts
// MUST use a Type with the `x-` prefix; their fields round-trip
// verbatim via raw.
//
// Producers should construct ContentPart via the typed constructors
// (TextPart, ToolCallPart, ToolResultPart, etc.) or by populating
// Type plus the matching variant fields directly.
type ContentPart struct {
	// Type discriminates the variant. See PartType* constants.
	Type string `json:"type"`

	// Metadata is free-form and round-trips on every variant.
	Metadata map[string]any `json:"metadata,omitempty"`

	// text variant.
	Text string `json:"-"`

	// tool_call variant.
	CallID       string          `json:"-"`
	Name         string          `json:"-"`
	Input        json.RawMessage `json:"-"`
	ParentCallID string          `json:"-"`

	// tool_result variant.
	// (CallID reused.)
	Output          json.RawMessage `json:"-"`
	IsError         bool            `json:"-"`
	ChildEnvelopeID string          `json:"-"`

	// image variant.
	Mime string `json:"-"`
	Data string `json:"-"`
	URL  string `json:"-"`
	Alt  string `json:"-"`

	// thinking variant.
	// (Text reused.)
	Signature string `json:"-"`

	// raw preserves the entire JSON for extension parts (Type prefixed
	// "x-") plus any unknown fields on known parts. On extension parts
	// raw IS the canonical form; on known parts raw is nil after a
	// round-trip-clean unmarshal.
	raw json.RawMessage
}

// TextPart constructs a text ContentPart.
func TextPart(text string) ContentPart {
	return ContentPart{Type: PartTypeText, Text: text}
}

// ToolCallPart constructs a tool_call ContentPart. input may be a
// json.RawMessage already-encoded, or any JSON-marshalable value.
func ToolCallPart(callID, name string, input any) (ContentPart, error) {
	raw, err := toRawJSON(input)
	if err != nil {
		return ContentPart{}, fmt.Errorf("stem: tool_call input: %w", err)
	}
	return ContentPart{
		Type:   PartTypeToolCall,
		CallID: callID,
		Name:   name,
		Input:  raw,
	}, nil
}

// ToolResultPart constructs a tool_result ContentPart.
func ToolResultPart(callID string, output any, isError bool) (ContentPart, error) {
	raw, err := toRawJSON(output)
	if err != nil {
		return ContentPart{}, fmt.Errorf("stem: tool_result output: %w", err)
	}
	return ContentPart{
		Type:    PartTypeToolResult,
		CallID:  callID,
		Output:  raw,
		IsError: isError,
	}, nil
}

// IsExtension reports whether Type is an x-prefixed extension.
// Returns true for any "x-" prefix; structural validity of the
// trailing token is enforced separately by isValidExtensionType,
// invoked from Validate.
func (c ContentPart) IsExtension() bool {
	return strings.HasPrefix(c.Type, "x-")
}

// isValidMimeType reports whether s matches the crtx v0.1 image.mime
// regex ^[a-z]+/[a-zA-Z0-9.+-]+$. type token is lowercase letters
// only; subtype permits alphanumerics plus ".", "+", "-".
func isValidMimeType(s string) bool {
	slash := strings.IndexByte(s, '/')
	if slash < 1 || slash == len(s)-1 {
		return false
	}
	for _, r := range s[:slash] {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	for _, r := range s[slash+1:] {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '+' || r == '-':
		default:
			return false
		}
	}
	return true
}

// isValidExtensionType reports whether s matches the crtx v0.1
// extension type regex ^x-[a-zA-Z0-9._-]+$. Requires at least one
// character after the "x-" prefix and restricts that character class
// to alphanumerics plus ".", "_", "-".
func isValidExtensionType(s string) bool {
	if len(s) < 3 || !strings.HasPrefix(s, "x-") {
		return false
	}
	for _, r := range s[2:] {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// clone deep-copies a ContentPart.
func (c ContentPart) clone() ContentPart {
	cp := c
	if c.Input != nil {
		cp.Input = append(json.RawMessage(nil), c.Input...)
	}
	if c.Output != nil {
		cp.Output = append(json.RawMessage(nil), c.Output...)
	}
	if c.raw != nil {
		cp.raw = append(json.RawMessage(nil), c.raw...)
	}
	if c.Metadata != nil {
		cp.Metadata = make(map[string]any, len(c.Metadata))
		for k, v := range c.Metadata {
			cp.Metadata[k] = v
		}
	}
	return cp
}

func toRawJSON(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage(`null`), nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return append(json.RawMessage(nil), raw...), nil
	}
	if raw, ok := v.([]byte); ok {
		// Validate it parses.
		var probe any
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, err
		}
		return append(json.RawMessage(nil), raw...), nil
	}
	return json.Marshal(v)
}

// MarshalJSON renders a ContentPart in crtx-canonical shape for its
// type discriminator. Extension parts emit their stored raw form.
func (c ContentPart) MarshalJSON() ([]byte, error) {
	if c.IsExtension() {
		if len(c.raw) == 0 {
			// Best-effort: emit just {"type": "..."} for an extension
			// part with no preserved body.
			return json.Marshal(struct {
				Type string `json:"type"`
			}{c.Type})
		}
		return c.raw, nil
	}

	switch c.Type {
	case PartTypeText:
		return json.Marshal(struct {
			Type     string         `json:"type"`
			Text     string         `json:"text"`
			Metadata map[string]any `json:"metadata,omitempty"`
		}{c.Type, c.Text, c.Metadata})

	case PartTypeToolCall:
		// Input is required by schema; ensure non-nil.
		input := c.Input
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		return json.Marshal(struct {
			Type         string          `json:"type"`
			CallID       string          `json:"call_id"`
			Name         string          `json:"name"`
			Input        json.RawMessage `json:"input"`
			ParentCallID string          `json:"parent_call_id,omitempty"`
			Metadata     map[string]any  `json:"metadata,omitempty"`
		}{c.Type, c.CallID, c.Name, input, c.ParentCallID, c.Metadata})

	case PartTypeToolResult:
		output := c.Output
		if len(output) == 0 {
			output = json.RawMessage(`null`)
		}
		// Use a buffer to control field order + omit is_error /
		// child_envelope_id when unset.
		var buf bytes.Buffer
		buf.WriteString(`{"type":"tool_result","call_id":`)
		callIDBytes, err := json.Marshal(c.CallID)
		if err != nil {
			return nil, err
		}
		buf.Write(callIDBytes)
		buf.WriteString(`,"output":`)
		buf.Write(output)
		if c.IsError {
			buf.WriteString(`,"is_error":true`)
		}
		if c.ChildEnvelopeID != "" {
			childBytes, err := json.Marshal(c.ChildEnvelopeID)
			if err != nil {
				return nil, err
			}
			buf.WriteString(`,"child_envelope_id":`)
			buf.Write(childBytes)
		}
		if len(c.Metadata) > 0 {
			metaBytes, err := json.Marshal(c.Metadata)
			if err != nil {
				return nil, err
			}
			buf.WriteString(`,"metadata":`)
			buf.Write(metaBytes)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil

	case PartTypeImage:
		// Schema requires exactly one of data/url. Reject the producer
		// when neither or both are set; silent emission of invalid
		// JSON is a footgun callers cannot see until validation runs.
		hasData := c.Data != ""
		hasURL := c.URL != ""
		if hasData == hasURL {
			return nil, fmt.Errorf("stem: image part: exactly one of data/url required")
		}
		type imageOut struct {
			Type     string         `json:"type"`
			Mime     string         `json:"mime"`
			Data     string         `json:"data,omitempty"`
			URL      string         `json:"url,omitempty"`
			Alt      string         `json:"alt,omitempty"`
			Metadata map[string]any `json:"metadata,omitempty"`
		}
		return json.Marshal(imageOut{
			Type: c.Type, Mime: c.Mime, Data: c.Data, URL: c.URL,
			Alt: c.Alt, Metadata: c.Metadata,
		})

	case PartTypeThinking:
		return json.Marshal(struct {
			Type      string         `json:"type"`
			Text      string         `json:"text"`
			Signature string         `json:"signature,omitempty"`
			Metadata  map[string]any `json:"metadata,omitempty"`
		}{c.Type, c.Text, c.Signature, c.Metadata})

	default:
		return nil, fmt.Errorf("stem: unknown ContentPart type %q (extension parts MUST use x- prefix)", c.Type)
	}
}

// UnmarshalJSON parses a ContentPart, populating the typed fields for
// known variants and preserving the raw JSON for extension parts.
func (c *ContentPart) UnmarshalJSON(data []byte) error {
	// Discriminator probe.
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("stem: ContentPart: %w", err)
	}
	if probe.Type == "" {
		return fmt.Errorf("stem: ContentPart: missing type")
	}
	c.Type = probe.Type

	if strings.HasPrefix(probe.Type, "x-") {
		c.raw = append(json.RawMessage(nil), data...)
		return nil
	}

	switch probe.Type {
	case PartTypeText:
		var v struct {
			Text     string         `json:"text"`
			Metadata map[string]any `json:"metadata,omitempty"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("stem: text part: %w", err)
		}
		c.Text = v.Text
		c.Metadata = v.Metadata
		return nil

	case PartTypeToolCall:
		var v struct {
			CallID       string          `json:"call_id"`
			Name         string          `json:"name"`
			Input        json.RawMessage `json:"input"`
			ParentCallID string          `json:"parent_call_id,omitempty"`
			Metadata     map[string]any  `json:"metadata,omitempty"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("stem: tool_call part: %w", err)
		}
		c.CallID = v.CallID
		c.Name = v.Name
		c.Input = v.Input
		c.ParentCallID = v.ParentCallID
		c.Metadata = v.Metadata
		return nil

	case PartTypeToolResult:
		var v struct {
			CallID          string          `json:"call_id"`
			Output          json.RawMessage `json:"output"`
			IsError         bool            `json:"is_error,omitempty"`
			ChildEnvelopeID string          `json:"child_envelope_id,omitempty"`
			Metadata        map[string]any  `json:"metadata,omitempty"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("stem: tool_result part: %w", err)
		}
		c.CallID = v.CallID
		c.Output = v.Output
		c.IsError = v.IsError
		c.ChildEnvelopeID = v.ChildEnvelopeID
		c.Metadata = v.Metadata
		return nil

	case PartTypeImage:
		var v struct {
			Mime     string         `json:"mime"`
			Data     string         `json:"data,omitempty"`
			URL      string         `json:"url,omitempty"`
			Alt      string         `json:"alt,omitempty"`
			Metadata map[string]any `json:"metadata,omitempty"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("stem: image part: %w", err)
		}
		c.Mime = v.Mime
		c.Data = v.Data
		c.URL = v.URL
		c.Alt = v.Alt
		c.Metadata = v.Metadata
		return nil

	case PartTypeThinking:
		var v struct {
			Text      string         `json:"text"`
			Signature string         `json:"signature,omitempty"`
			Metadata  map[string]any `json:"metadata,omitempty"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("stem: thinking part: %w", err)
		}
		c.Text = v.Text
		c.Signature = v.Signature
		c.Metadata = v.Metadata
		return nil

	default:
		return fmt.Errorf("stem: ContentPart: unknown type %q (extension parts MUST use x- prefix)", probe.Type)
	}
}
