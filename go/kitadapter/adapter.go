// Package kitadapter documents the mapping between hop.top/kit/llm
// types and hop.top/stem types. The real implementation lives in kit
// (which imports stem), since stem cannot import kit.
//
// # Mapping
//
//	AsProvider(llm.Client) stem.Provider
//	  - llm.Client.Chat       → stem.Provider.Complete
//	  - llm.Client.StreamChat → stem.Provider.Stream
//	  - llm.Client.ChatWith   → stem.Provider.CallWithTools
//
//	llm.Message  ↔ stem.Turn
//	  - llm.Message.Role     ↔ stem.Turn.Role
//	  - llm.Message.Content  ↔ stem.Turn.Content[0] (text part)
//
//	llm.ToolDef  ↔ stem.ToolDef
//	llm.Response → stem.Turn (role=assistant, content=[text])
//	  - llm.Response.Usage → stem.Turn.Metadata["usage"]
//
//	llm.TokenIterator → stem.TurnStream
package kitadapter

import (
	"context"
	"encoding/json"

	"hop.top/stem"
)

// LLMClient is the subset of llm.Client that the adapter needs.
// This mirrors the kit llm.Client interface for documentation and
// local testing without importing kit.
type LLMClient interface {
	Chat(ctx context.Context, msgs []Message) (Response, error)
	StreamChat(ctx context.Context, msgs []Message) (TokenIterator, error)
	ChatWith(ctx context.Context, msgs []Message, tools []ToolDef) (Response, error)
}

// Message mirrors llm.Message for local testing.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Response mirrors llm.Response for local testing.
type Response struct {
	Message Message        `json:"message"`
	Usage   map[string]int `json:"usage,omitempty"`
}

// ToolDef mirrors llm.ToolDef for local testing.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// TokenIterator mirrors llm.TokenIterator for local testing.
type TokenIterator interface {
	Next() (string, bool, error)
	Close() error
}

// Adapter wraps an LLMClient to implement stem.Provider.
type Adapter struct {
	client LLMClient
}

// AsProvider wraps an LLMClient as a stem.Provider.
func AsProvider(c LLMClient) stem.Provider {
	return &Adapter{client: c}
}

// Complete implements stem.Provider.
func (a *Adapter) Complete(
	ctx context.Context, turns []stem.Turn,
) (stem.Turn, error) {
	msgs := turnsToMessages(turns)
	resp, err := a.client.Chat(ctx, msgs)
	if err != nil {
		return stem.Turn{}, err
	}
	return responseToTurn(resp), nil
}

// Stream implements stem.Provider.
func (a *Adapter) Stream(
	ctx context.Context, turns []stem.Turn,
) (stem.TurnStream, error) {
	msgs := turnsToMessages(turns)
	iter, err := a.client.StreamChat(ctx, msgs)
	if err != nil {
		return nil, err
	}
	return &streamAdapter{iter: iter}, nil
}

// CallWithTools implements stem.Provider.
func (a *Adapter) CallWithTools(
	ctx context.Context, turns []stem.Turn, tools []stem.ToolDef,
) (stem.Turn, error) {
	msgs := turnsToMessages(turns)
	llmTools := stemToolsToLLM(tools)
	resp, err := a.client.ChatWith(ctx, msgs, llmTools)
	if err != nil {
		return stem.Turn{}, err
	}
	return responseToTurn(resp), nil
}

// --- mapping helpers ---

func turnsToMessages(turns []stem.Turn) []Message {
	msgs := make([]Message, 0, len(turns))
	for _, t := range turns {
		// Flatten Content[] into a text-only string for the kit llm
		// wire (which is text-only for v0.1).
		msgs = append(msgs, Message{
			Role:    string(t.Role),
			Content: flattenText(t.Content),
		})
	}
	return msgs
}

func flattenText(parts []stem.ContentPart) string {
	var out string
	for _, p := range parts {
		if p.Type == stem.PartTypeText {
			out += p.Text
		}
	}
	return out
}

func responseToTurn(resp Response) stem.Turn {
	t := stem.Turn{
		Role:    stem.Role(resp.Message.Role),
		Content: []stem.ContentPart{stem.TextPart(resp.Message.Content)},
	}
	if resp.Usage != nil {
		t.Metadata = map[string]any{"usage": resp.Usage}
	}
	return t
}

func stemToolsToLLM(tools []stem.ToolDef) []ToolDef {
	out := make([]ToolDef, len(tools))
	for i, t := range tools {
		out[i] = ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return out
}

// streamAdapter wraps TokenIterator as stem.TurnStream.
type streamAdapter struct {
	iter TokenIterator
}

func (s *streamAdapter) Next() (stem.Token, error) {
	text, done, err := s.iter.Next()
	if err != nil {
		return stem.Token{}, err
	}
	return stem.Token{Text: text, Done: done}, nil
}

func (s *streamAdapter) Close() error {
	return s.iter.Close()
}
