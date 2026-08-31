// current-time — smallest viable stem agent loop.
//
// Demonstrates manual wiring of a stem Envelope through one tool-use
// round-trip against the Anthropic Messages API:
//
//  1. Construct a stem.Session via stem.NewSession.
//  2. Append a user turn asking the time.
//  3. Call Anthropic with a single `current_time` tool definition.
//  4. Convert the assistant's tool_use block into a stem ContentPart
//     and append an assistant turn.
//  5. Execute the tool locally and append a tool-role turn carrying a
//     tool_result ContentPart.
//  6. Call Anthropic again with the updated transcript.
//  7. Append the model's final text response.
//  8. Serialize the Session to ./session.jsonl (one envelope per
//     line — matches the crtx JSONL convention).
//  9. Reload from disk, decode, and stem.Validate the round-trip.
//
// The Anthropic HTTP transport is wrapped with xrr, so the same code
// records cassettes (XRR_MODE=record + ANTHROPIC_API_KEY) and replays
// them deterministically (default mode, no key needed).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"hop.top/stem"
)

const (
	cassetteDir = "./cassettes"
	model       = anthropic.ModelClaudeSonnet4_6
	maxTokens   = 1024
	userPrompt  = "What time is it?"
	toolName    = "current_time"
	jsonlPath   = "./session.jsonl"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("current-time: %v", err)
	}
}

func run(ctx context.Context) error {
	// --- 1. xrr session.
	absCassetteDir, err := filepath.Abs(cassetteDir)
	if err != nil {
		return fmt.Errorf("resolve cassette dir: %w", err)
	}
	if err := os.MkdirAll(absCassetteDir, 0o755); err != nil {
		return fmt.Errorf("create cassette dir: %w", err)
	}

	xrrSess, err := loadSession(ctx, absCassetteDir, os.Getenv("XRR_MODE"))
	if err != nil {
		return err
	}
	defer xrrSess.Close()

	// Empty API key is fine in replay mode — the SDK never reaches the
	// network. Record mode requires a real key.
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = "replay-mode-no-key-required"
	}

	httpClient := newXRRClient(xrrSess, nil)
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
	)

	// --- 2. fresh stem Session + initial user turn.
	sess := stem.NewSession("current-time-demo")
	sess.Turns = append(sess.Turns, stem.Turn{
		ID:        "t-user-0",
		Role:      stem.RoleUser,
		CreatedAt: time.Unix(0, 0).UTC(), // fixed timestamp keeps the example deterministic
		Content:   []stem.ContentPart{stem.TextPart(userPrompt)},
	})

	// --- 3. tool definition + first Anthropic call.
	tool := anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        toolName,
			Description: anthropic.String("Returns the current UTC time as an ISO 8601 string."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{},
				Required:   []string{},
			},
		},
	}

	first, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		Tools: []anthropic.ToolUnionParam{tool},
	})
	if err != nil {
		return fmt.Errorf("anthropic first call: %w", err)
	}

	// --- 4. project the assistant turn into a stem turn.
	assistantTurn, toolUseID, toolInput, err := assistantTurnFromMessage(first, "t-assistant-1")
	if err != nil {
		return fmt.Errorf("project assistant turn: %w", err)
	}
	sess.Turns = append(sess.Turns, assistantTurn)

	if toolUseID == "" {
		// Model decided not to call the tool — surface a clear message
		// instead of silently swallowing the unexpected path. With the
		// vendored cassette this never fires.
		return fmt.Errorf("model returned no tool_use; cassette/model out of sync")
	}

	// --- 5. execute tool locally + append tool-role turn.
	toolResult := executeCurrentTime(toolInput)
	resultPart, err := stem.ToolResultPart(toolUseID, toolResult, false)
	if err != nil {
		return fmt.Errorf("tool_result part: %w", err)
	}
	sess.Turns = append(sess.Turns, stem.Turn{
		ID:        "t-tool-2",
		Role:      stem.RoleTool,
		CreatedAt: time.Unix(0, 0).UTC(),
		Content:   []stem.ContentPart{resultPart},
	})

	// --- 6. second Anthropic call with the tool result echoed back.
	resultJSON, err := json.Marshal(toolResult)
	if err != nil {
		return fmt.Errorf("marshal tool result: %w", err)
	}

	second, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
			anthropic.NewAssistantMessage(messageBlocks(first)...),
			anthropic.NewUserMessage(anthropic.NewToolResultBlock(
				toolUseID, string(resultJSON), false,
			)),
		},
		Tools: []anthropic.ToolUnionParam{tool},
	})
	if err != nil {
		return fmt.Errorf("anthropic second call: %w", err)
	}

	// --- 7. append final assistant text.
	finalTurn, _, _, err := assistantTurnFromMessage(second, "t-assistant-3")
	if err != nil {
		return fmt.Errorf("project final turn: %w", err)
	}
	sess.Turns = append(sess.Turns, finalTurn)

	// --- 8. write ./session.jsonl (one envelope per line).
	if err := writeJSONL(jsonlPath, sess); err != nil {
		return fmt.Errorf("write jsonl: %w", err)
	}
	fmt.Printf("wrote %s (%d turns)\n", jsonlPath, len(sess.Turns))

	// --- 9. reload + validate.
	reloaded, err := readJSONL(jsonlPath)
	if err != nil {
		return fmt.Errorf("reload jsonl: %w", err)
	}
	if err := stem.Validate(reloaded); err != nil {
		return fmt.Errorf("validate reloaded envelope: %w", err)
	}
	fmt.Println("envelope round-trip + validate: ok")
	return nil
}

// assistantTurnFromMessage projects an Anthropic Message into a stem
// assistant Turn and surfaces the first tool_use id+input found, if any.
func assistantTurnFromMessage(m *anthropic.Message, turnID string) (stem.Turn, string, map[string]any, error) {
	parts := make([]stem.ContentPart, 0, len(m.Content))
	var (
		toolID    string
		toolInput map[string]any
	)
	for _, block := range m.Content {
		switch block.Type {
		case "text":
			parts = append(parts, stem.TextPart(block.Text))
		case "tool_use":
			var input map[string]any
			if len(block.Input) > 0 {
				if err := json.Unmarshal(block.Input, &input); err != nil {
					return stem.Turn{}, "", nil, fmt.Errorf("decode tool_use input: %w", err)
				}
			} else {
				input = map[string]any{}
			}
			part, err := stem.ToolCallPart(block.ID, block.Name, input)
			if err != nil {
				return stem.Turn{}, "", nil, err
			}
			parts = append(parts, part)
			if toolID == "" {
				toolID = block.ID
				toolInput = input
			}
		}
	}
	if len(parts) == 0 {
		// stem.Validate rejects empty content; surface a clear error.
		return stem.Turn{}, "", nil, fmt.Errorf("assistant message had no usable content blocks")
	}
	return stem.Turn{
		ID:        turnID,
		Role:      stem.RoleAssistant,
		CreatedAt: time.Unix(0, 0).UTC(),
		Content:   parts,
	}, toolID, toolInput, nil
}

// messageBlocks converts a returned Anthropic Message back into the
// param-shaped blocks needed to echo the assistant turn on the next call.
func messageBlocks(m *anthropic.Message) []anthropic.ContentBlockParamUnion {
	out := make([]anthropic.ContentBlockParamUnion, 0, len(m.Content))
	for _, block := range m.Content {
		switch block.Type {
		case "text":
			out = append(out, anthropic.NewTextBlock(block.Text))
		case "tool_use":
			var input any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &input)
			}
			out = append(out, anthropic.NewToolUseBlock(block.ID, input, block.Name))
		}
	}
	return out
}

// executeCurrentTime is the local handler for the current_time tool.
// Uses a fixed timestamp so the recorded cassette stays stable across
// re-records; production code would call time.Now().
func executeCurrentTime(_ map[string]any) map[string]any {
	return map[string]any{
		"time": "2026-01-01T00:00:00Z",
		"zone": "UTC",
	}
}

// writeJSONL writes the session as one JSON object per line.
func writeJSONL(path string, sess *stem.Session) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode(sess)
}

// readJSONL reads a single-envelope JSONL file and decodes it.
func readJSONL(path string) (*stem.Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sess stem.Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}
