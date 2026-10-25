package llmclient

import (
	"encoding/json"
	"regexp"
	"strings"

	"goodfood/assistant-service/internal/domain"
)

// Small local models (observed with qwen2.5:7b on Ollama) sometimes write the
// function call as plain text — `propose_order("items": [...], ...)` — with
// finish_reason "stop" and no structured tool_calls. Left alone, that text
// reaches the customer verbatim and no order is ever proposed. The helpers
// below recover the call when its arguments can be read unambiguously, and
// flag it otherwise so it is never shown raw.

// toolCallStart matches a pseudo call to one of the offered tools: the name,
// then "(", then something that starts an argument.
func toolCallStart(tools []domain.ToolDefinition) *regexp.Regexp {
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, regexp.QuoteMeta(t.Name))
	}
	return regexp.MustCompile(`(?i)\b(` + strings.Join(names, "|") + `)\s*\(\s*(["{\[\w])`)
}

// looksLikeToolCall reports whether content contains a pseudo call to an
// offered tool, whether or not its arguments can be recovered.
func looksLikeToolCall(content string, tools []domain.ToolDefinition) bool {
	if len(tools) == 0 {
		return false
	}
	if _, _, ok := jsonCallShape(content); ok {
		return true
	}
	if _, ok := embeddedArguments(content, tools); ok {
		return true
	}
	re := toolCallStart(tools)
	return re != nil && re.MatchString(content)
}

// jsonCallShape detects the call format native to Llama-family models,
// {"name": "...", "parameters": {...}} (some spell the second key
// "arguments"), wherever it sits in the message — models sometimes put prose
// or junk before it. It reports the tool name and the raw arguments object.
func jsonCallShape(content string) (name string, args map[string]any, ok bool) {
	for _, obj := range embeddedObjects(content) {
		n, _ := obj["name"].(string)
		if n == "" {
			continue
		}
		if p, isObj := obj["parameters"].(map[string]any); isObj {
			return n, p, true
		}
		if a, isObj := obj["arguments"].(map[string]any); isObj {
			return n, a, true
		}
	}
	return "", nil, false
}

// embeddedObjects returns the top-level JSON objects found anywhere in text.
func embeddedObjects(content string) []map[string]any {
	var out []map[string]any
	for i := 0; i < len(content); i++ {
		if content[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(content[i:]))
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil || len(obj) == 0 {
			continue
		}
		out = append(out, obj)
		i += int(dec.InputOffset()) - 1 // skip what was consumed: nested objects are not top-level
	}
	return out
}

// embeddedArguments finds a JSON object that is a complete set of arguments
// for an offered tool (every required parameter present) — the model narrated
// the order and pasted the arguments into its sentence instead of calling.
func embeddedArguments(content string, tools []domain.ToolDefinition) (*domain.ToolCall, bool) {
	for _, obj := range embeddedObjects(content) {
		for _, t := range tools {
			required := requiredKeys(t)
			if len(required) == 0 {
				continue
			}
			complete := true
			for _, k := range required {
				if _, ok := obj[k]; !ok {
					complete = false
					break
				}
			}
			if !complete {
				continue
			}
			raw, err := json.Marshal(obj)
			if err != nil {
				return nil, true
			}
			return &domain.ToolCall{Name: t.Name, Arguments: string(raw)}, true
		}
	}
	return nil, false
}

func requiredKeys(t domain.ToolDefinition) []string {
	switch r := t.Parameters["required"].(type) {
	case []string:
		return r
	case []any:
		keys := make([]string, 0, len(r))
		for _, k := range r {
			if s, ok := k.(string); ok {
				keys = append(keys, s)
			}
		}
		return keys
	}
	return nil
}

// recoverTextToolCall turns a textual pseudo call into a real ToolCall. It only
// ever recovers tools that were actually offered for this request, and only
// when every argument parses as JSON.
func recoverTextToolCall(content string, tools []domain.ToolDefinition) *domain.ToolCall {
	if len(tools) == 0 {
		return nil
	}
	if name, args, ok := jsonCallShape(content); ok {
		for _, t := range tools {
			if strings.EqualFold(t.Name, name) {
				raw, err := json.Marshal(args)
				if err != nil {
					return nil
				}
				return &domain.ToolCall{Name: t.Name, Arguments: string(raw)}
			}
		}
		return nil // a call to a tool that was never offered: never recovered
	}
	re := toolCallStart(tools)
	if re == nil {
		return nil
	}
	loc := re.FindStringSubmatchIndex(content)
	if loc == nil {
		tc, _ := embeddedArguments(content, tools)
		return tc
	}
	var tool *domain.ToolDefinition
	for i := range tools {
		if strings.EqualFold(tools[i].Name, content[loc[2]:loc[3]]) {
			tool = &tools[i]
			break
		}
	}
	if tool == nil {
		return nil
	}

	// loc[4] is where the first argument character (capture group 2) begins.
	body, ok := balancedArguments(content[loc[4]:])
	if !ok {
		return nil
	}
	args := parseArguments(strings.TrimSpace(body), *tool)
	if args == nil {
		return nil
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil
	}
	return &domain.ToolCall{Name: tool.Name, Arguments: string(raw)}
}

// balancedArguments returns the text up to the ")" that closes the call,
// ignoring brackets inside JSON strings.
func balancedArguments(s string) (string, bool) {
	depth, inStr, escaped := 0, false, false
	for i, r := range s {
		switch {
		case escaped:
			escaped = false
		case inStr && r == '\\':
			escaped = true
		case r == '"':
			inStr = !inStr
		case inStr:
		case r == '(' || r == '[' || r == '{':
			depth++
		case r == ']' || r == '}':
			depth--
		case r == ')':
			if depth == 0 {
				return s[:i], true
			}
			depth--
		}
	}
	return "", false
}

// parseArguments accepts the three shapes seen in practice: a single JSON
// object, `"key": value, ...` pairs, or bare positional values.
func parseArguments(body string, tool domain.ToolDefinition) map[string]any {
	if body == "" {
		return nil
	}
	if strings.HasPrefix(body, "{") {
		var obj map[string]any
		if json.Unmarshal([]byte(body), &obj) == nil && len(obj) > 0 {
			return obj
		}
	}

	out := map[string]any{}
	var positional []any
	for _, part := range splitTopLevel(body) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if key, val, ok := splitKeyValue(part); ok {
			var v any
			if json.Unmarshal([]byte(val), &v) != nil {
				return nil
			}
			out[key] = v
			continue
		}
		var v any
		if json.Unmarshal([]byte(part), &v) != nil {
			return nil
		}
		positional = append(positional, v)
	}
	if !assignPositional(out, positional, tool) || len(out) == 0 {
		return nil
	}
	return out
}

var keyValue = regexp.MustCompile(`^"?([A-Za-z_][A-Za-z0-9_]*)"?\s*:\s*(.+)$`)

func splitKeyValue(part string) (string, string, bool) {
	m := keyValue.FindStringSubmatch(part)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// splitTopLevel splits on commas that are outside strings and brackets.
func splitTopLevel(s string) []string {
	var parts []string
	depth, inStr, escaped, start := 0, false, false, 0
	for i, r := range s {
		switch {
		case escaped:
			escaped = false
		case inStr && r == '\\':
			escaped = true
		case r == '"':
			inStr = !inStr
		case inStr:
		case r == '(' || r == '[' || r == '{':
			depth++
		case r == ')' || r == ']' || r == '}':
			depth--
		case r == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// assignPositional maps bare values onto the parameters of the tool schema by
// JSON type. It refuses to guess when a type matches more than one parameter.
func assignPositional(out map[string]any, positional []any, tool domain.ToolDefinition) bool {
	if len(positional) == 0 {
		return true
	}
	props, _ := tool.Parameters["properties"].(map[string]any)
	for _, v := range positional {
		want := jsonType(v)
		var match string
		for name, p := range props {
			if _, taken := out[name]; taken {
				continue
			}
			if pm, ok := p.(map[string]any); ok && pm["type"] == want {
				if match != "" {
					return false
				}
				match = name
			}
		}
		if match == "" {
			return false
		}
		out[match] = v
	}
	return true
}

func jsonType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case bool:
		return "boolean"
	case float64:
		return "number"
	}
	return ""
}
