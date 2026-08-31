// web-search — second stem agent example.
//
// Same envelope-wiring shape as ../current-time/, but the tool is
// `web_search`, executed by calling the Tavily search API
// (https://api.tavily.com). One stem.Session captures the full
// transcript; two upstream services (Anthropic + Tavily) are recorded
// into the same xrr cassette directory because each HTTP call has a
// distinct method+URL+body fingerprint.
//
// Steps:
//
//  1. Construct a fresh stem.Session.
//  2. Append a user turn asking for the latest stable Go release.
//  3. Call Anthropic with a `web_search(query)` tool.
//  4. Convert the assistant's tool_use block into a stem ContentPart
//     of type tool_call.
//  5. Execute the tool: POST to api.tavily.com (xrr-wrapped client).
//  6. Append a tool-role turn carrying a tool_result ContentPart.
//  7. Call Anthropic again with the tool result echoed back.
//  8. Append the model's final text response.
//  9. Serialize the Session to ./session.jsonl and reload+validate.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"hop.top/stem"
)

const (
	cassetteDir     = "./cassettes"
	model           = anthropic.ModelClaudeSonnet4_6
	maxTokens       = 1024
	userPrompt      = "What's the latest stable Go release?"
	toolName        = "web_search"
	tavilyURL       = "https://api.tavily.com/search"
	maxTavilyResult = 3
	jsonlPath       = "./session.jsonl"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("web-search: %v", err)
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

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = "replay-mode-no-key-required"
	}
	tavilyKey := os.Getenv("TAVILY_API_KEY")
	if tavilyKey == "" {
		tavilyKey = "replay-mode-no-key-required"
	}

	httpClient := newXRRClient(xrrSess, nil)
	anthropicClient := anthropic.NewClient(
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
	)

	// --- 2. fresh stem Session + initial user turn.
	sess := stem.NewSession("web-search-demo")
	sess.Turns = append(sess.Turns, stem.Turn{
		ID:        "t-user-0",
		Role:      stem.RoleUser,
		CreatedAt: time.Unix(0, 0).UTC(),
		Content:   []stem.ContentPart{stem.TextPart(userPrompt)},
	})

	// --- 3. tool definition + first Anthropic call.
	tool := anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        toolName,
			Description: anthropic.String("Run a web search and return the top results."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query string.",
					},
				},
				Required: []string{"query"},
			},
		},
	}

	first, err := anthropicClient.Messages.New(ctx, anthropic.MessageNewParams{
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
		return fmt.Errorf("model returned no tool_use; cassette/model out of sync")
	}

	// --- 5. execute tool against Tavily.
	query, _ := toolInput["query"].(string)
	if query == "" {
		query = userPrompt
	}
	results, err := callTavily(ctx, httpClient, tavilyKey, query)
	if err != nil {
		return fmt.Errorf("tavily: %w", err)
	}

	resultPart, err := stem.ToolResultPart(toolUseID, map[string]any{
		"query":   query,
		"results": results,
	}, false)
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
	resultJSON, err := json.Marshal(map[string]any{
		"query":   query,
		"results": results,
	})
	if err != nil {
		return fmt.Errorf("marshal tool result: %w", err)
	}

	second, err := anthropicClient.Messages.New(ctx, anthropic.MessageNewParams{
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

	// --- 8. write ./session.jsonl.
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

// tavilyResult mirrors the subset of fields the example surfaces.
type tavilyResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// callTavily POSTs the query to api.tavily.com via the xrr-wrapped
// client. The request body matches Tavily's documented schema (api_key,
// query, max_results); the response is normalized into a 3-tuple shape
// that's stable across cassettes.
func callTavily(ctx context.Context, client *xrrClient, apiKey, query string) ([]tavilyResult, error) {
	body, err := json.Marshal(map[string]any{
		"api_key":     apiKey,
		"query":       query,
		"max_results": maxTavilyResult,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilyURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tavily: status %d: %s", resp.StatusCode, string(respBody))
	}
	// Tavily's actual response has a "results" array of objects with
	// "title", "url", and "content" fields. Map "content" -> snippet.
	var raw struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("tavily: decode: %w", err)
	}
	out := make([]tavilyResult, 0, len(raw.Results))
	for _, r := range raw.Results {
		out = append(out, tavilyResult{Title: r.Title, URL: r.URL, Snippet: r.Content})
	}
	return out, nil
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
