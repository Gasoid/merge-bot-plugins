// Package wire holds the Anthropic Messages API request and response types.
//
// It deliberately imports nothing but encoding/json. The plugin's main package
// imports github.com/extism/go-pdk, whose host functions have no bodies outside
// wasm, so no test in that package can run on the host. Keeping the wire format
// here means its encoding is covered by executable tests, via
// `go test ./plugins/claude-reviewer/internal/...` (a bare `go test ./...` from
// the repository root still fails to build the pdk-importing packages).
package wire

import "encoding/json"

type Request struct {
	Model      string      `json:"model"`
	MaxTokens  int         `json:"max_tokens"`
	Messages   []Message   `json:"messages"`
	System     string      `json:"system,omitempty"`
	Tools      []Tool      `json:"tools,omitempty"`
	ToolChoice *ToolChoice `json:"tool_choice,omitempty"`
}

// ToolChoice with Type "none" leaves tools defined but forbids calling them.
// Dropping tools instead is not an option: the API rejects any request whose
// messages contain tool_use or tool_result blocks without a tools definition.
type ToolChoice struct {
	Type string `json:"type"`
}

type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock is used both for decoding response content and for encoding it
// back as an assistant message on the next turn. Every field the API may send
// on a block type we echo has to be represented here, or the round trip loses
// it. See MarshalJSON: the omitempty tags below apply only to block types this
// package does not know about.
type ContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
	// Thinking and Signature carry a thinking block. Current models think by
	// default, so a tool-calling turn returns thinking blocks alongside the
	// tool_use, and they must be replayed unchanged: the API rejects a thinking
	// block whose contents were modified or whose signature is missing. With the
	// default display of "omitted" the text is empty but the signature is not,
	// which is why Thinking must serialize even when blank.
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// Data carries a redacted_thinking block, which is opaque and replayed as-is.
	Data string `json:"data,omitempty"`
}

// MarshalJSON writes exactly the fields the API requires for each block type.
// Marshalling the struct directly is not enough: input is required on a tool_use
// block, text on a text block, and thinking plus signature on a thinking block,
// but omitempty drops them whenever a tool takes no arguments or the text is
// empty. Because response blocks are echoed back verbatim as assistant
// messages, that would make the following request fail.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	switch b.Type {
	case "text":
		return json.Marshal(textBlock{Type: b.Type, Text: b.Text})
	case "tool_use":
		input := b.Input
		if input == nil {
			input = map[string]interface{}{}
		}
		return json.Marshal(toolUseBlock{Type: b.Type, ID: b.ID, Name: b.Name, Input: input})
	case "tool_result":
		return json.Marshal(toolResultBlock{Type: b.Type, ToolUseID: b.ToolUseID, Content: b.Content})
	case "thinking":
		return json.Marshal(thinkingBlock{Type: b.Type, Thinking: b.Thinking, Signature: b.Signature})
	case "redacted_thinking":
		return json.Marshal(redactedThinkingBlock{Type: b.Type, Data: b.Data})
	default:
		return json.Marshal(contentBlockFields(b))
	}
}

// contentBlockFields marshals via the struct tags without recursing back into
// ContentBlock.MarshalJSON.
type contentBlockFields ContentBlock

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolUseBlock struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

type thinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

type redactedThinkingBlock struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"input_schema"`
}

// InputSchema.Properties has no omitempty: a tool that takes no arguments must
// still send "properties": {}, since the API rejects an object schema without
// the key.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type Response struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
}
