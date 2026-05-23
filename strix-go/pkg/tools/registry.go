package tools

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type XMLParameter struct {
	Name        string `xml:"name,attr"`
	Type        string `xml:"type,attr"`
	Required    string `xml:"required,attr"`
	Description string `xml:",chardata"`
}

type XMLTool struct {
	Name        string         `xml:"name,attr"`
	Description string         `xml:"description"`
	Parameters  []XMLParameter `xml:"parameters>parameter"`
	Returns     struct {
		Type        string `xml:"type,attr"`
		Description string `xml:",chardata"`
	} `xml:"returns"`
	Notes    string `xml:"notes"`
	Examples string `xml:"examples"`
}

type XMLTools struct {
	XMLName xml.Name  `xml:"tools"`
	Tools   []XMLTool `xml:"tool"`
}

// ExecutionContext defines where a tool runs
type ExecutionContext string

const (
	ExecutionContextSandbox ExecutionContext = "sandbox"
	ExecutionContextParent  ExecutionContext = "parent"
)

type ToolDefinition struct {
	Name             string
	SandboxExecution bool
	Handler          interface{} // Go function for local tools
	XML              string      // Raw XML snippet for system prompt injection
	Parsed           XMLTool
}

// IsAvailableInContext returns true if this tool can be executed in the given context
func (td ToolDefinition) IsAvailableInContext(ctx ExecutionContext) bool {
	switch ctx {
	case ExecutionContextSandbox:
		// Only tools marked with sandbox_execution=true are available in sandbox
		return td.SandboxExecution
	case ExecutionContextParent:
		// All tools available to parent
		return true
	default:
		return false
	}
}

var (
	registryLock sync.RWMutex
	toolRegistry = make(map[string]ToolDefinition)
)

func Register(name string, sandbox bool, handler interface{}) {
	registryLock.Lock()
	defer registryLock.Unlock()

	def, exists := toolRegistry[name]
	if exists {
		def.SandboxExecution = sandbox
		def.Handler = handler
		toolRegistry[name] = def
	} else {
		toolRegistry[name] = ToolDefinition{
			Name:             name,
			SandboxExecution: sandbox,
			Handler:          handler,
		}
	}
}

func LoadSchemaFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read schema file %s: %w", filePath, err)
	}
	return LoadSchema(data)
}

// toolBlockRegex extracts <tool name="...">...</tool> blocks. Non-greedy on body
// so <examples> containing other markup won't swallow following tools. Schema
// authors are expected to keep </tool> only at structural ends.
var toolBlockRegex = regexp.MustCompile(`(?s)<tool\s+name="[^"]+"[^>]*>.*?</tool>`)

// examplesBlockRegex strips the body of <examples> so the schemas' embedded
// <function=name>/<parameter=name> demo syntax doesn't make the document
// invalid XML.
var examplesBlockRegex = regexp.MustCompile(`(?s)<examples>.*?</examples>`)

// LoadSchema parses a tools schema file and registers tool metadata. The schemas
// contain XML-shaped definitions plus an <examples> section that holds non-XML
// demo syntax, and descriptions may contain unescaped '&' characters. We extract
// tool blocks with a tolerant regex, sanitize the parts that aren't valid XML,
// then run the cleaned block through encoding/xml so XMLTool/XMLParameter fields
// (used by NormalizeArguments) are populated from real structure rather than
// secondary regex scrapes.
func LoadSchema(data []byte) error {
	registryLock.Lock()
	defer registryLock.Unlock()

	rawToolBlocks := toolBlockRegex.FindAllString(string(data), -1)
	if len(rawToolBlocks) == 0 {
		return fmt.Errorf("no tools found in schema")
	}

	var parseErrors []string
	registered := 0

	for _, rawXML := range rawToolBlocks {
		sanitized := sanitizeForXMLParsing(rawXML)

		var xmlTool XMLTool
		if err := xml.Unmarshal([]byte(sanitized), &xmlTool); err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("xml unmarshal: %v", err))
			continue
		}
		if xmlTool.Name == "" {
			parseErrors = append(parseErrors, "tool missing name attribute")
			continue
		}

		def, exists := toolRegistry[xmlTool.Name]
		if exists {
			def.Parsed = xmlTool
			def.XML = rawXML
			toolRegistry[xmlTool.Name] = def
		} else {
			toolRegistry[xmlTool.Name] = ToolDefinition{
				Name:             xmlTool.Name,
				SandboxExecution: true, // Default to sandbox execution
				XML:              rawXML,
				Parsed:           xmlTool,
			}
		}
		registered++
	}

	if registered == 0 {
		return fmt.Errorf("failed to parse any tools: %s", strings.Join(parseErrors, "; "))
	}
	return nil
}

// sanitizeForXMLParsing rewrites a raw <tool>...</tool> block into a form that
// encoding/xml will accept. Two transformations:
//   1. Replace the body of <examples>...</examples> with empty content. The
//      authored examples use <function=name>/<parameter=name> syntax that the
//      LLM emits at runtime — it's intentional non-XML and must not be parsed.
//   2. Escape lone '&' characters that aren't already part of an entity. Some
//      descriptions contain prose like "use & for backgrounding".
func sanitizeForXMLParsing(raw string) string {
	cleaned := examplesBlockRegex.ReplaceAllString(raw, "<examples></examples>")
	return escapeLoneAmpersands(cleaned)
}

// escapeLoneAmpersands replaces '&' with '&amp;' unless it's already starting a
// recognized XML entity (&amp; &lt; &gt; &quot; &apos; or &#NNN; / &#xHH;).
func escapeLoneAmpersands(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		if entityEnd := matchEntity(s, i); entityEnd > 0 {
			b.WriteString(s[i:entityEnd])
			i = entityEnd - 1 // for-loop will increment
			continue
		}
		b.WriteString("&amp;")
	}
	return b.String()
}

// matchEntity reports the index past the end of a valid XML entity starting at
// s[start] (s[start] == '&'). Returns 0 if no valid entity is found within a
// short lookahead window.
func matchEntity(s string, start int) int {
	const maxEntityLen = 10
	end := start + 1
	limit := start + maxEntityLen
	if limit > len(s) {
		limit = len(s)
	}
	for end < limit && s[end] != ';' {
		end++
	}
	if end >= limit || s[end] != ';' {
		return 0
	}
	body := s[start+1 : end]
	switch body {
	case "amp", "lt", "gt", "quot", "apos":
		return end + 1
	}
	if len(body) > 1 && body[0] == '#' {
		digits := body[1:]
		hex := false
		if digits[0] == 'x' || digits[0] == 'X' {
			hex = true
			digits = digits[1:]
			if len(digits) == 0 {
				return 0
			}
		}
		for _, c := range digits {
			if hex {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
					return 0
				}
			} else {
				if c < '0' || c > '9' {
					return 0
				}
			}
		}
		return end + 1
	}
	return 0
}

func GetTool(name string) (ToolDefinition, bool) {
	registryLock.RLock()
	defer registryLock.RUnlock()
	def, exists := toolRegistry[name]
	return def, exists
}

func NormalizeArguments(def ToolDefinition, kwargs map[string]interface{}) map[string]interface{} {
	if len(kwargs) == 0 {
		return kwargs
	}

	normalized := make(map[string]interface{}, len(kwargs))
	paramTypes := make(map[string]string, len(def.Parsed.Parameters))
	for _, param := range def.Parsed.Parameters {
		paramTypes[param.Name] = strings.ToLower(strings.TrimSpace(param.Type))
	}

	for key, value := range kwargs {
		normalized[key] = normalizeArgumentValue(key, value, paramTypes[key])
	}

	return normalized
}

func normalizeArgumentValue(name string, value interface{}, declaredType string) interface{} {
	strValue, ok := value.(string)
	if !ok {
		return value
	}

	trimmed := strings.TrimSpace(strValue)
	if trimmed == "" {
		return strValue
	}

	switch declaredType {
	case "boolean", "bool":
		return parseBool(trimmed)
	case "integer", "int":
		if parsed, err := strconv.Atoi(trimmed); err == nil {
			return parsed
		}
	case "number", "float":
		if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return parsed
		}
	}

	switch name {
	case "tags", "todo_ids", "findings", "final_recommendations":
		if parsed, ok := parseJSONValue(trimmed); ok {
			return parsed
		}
		if strings.Contains(trimmed, ",") {
			return splitAndTrim(trimmed, ",")
		}
		return []interface{}{trimmed}
	case "updates":
		if parsed, ok := parseJSONValue(trimmed); ok {
			return parsed
		}
	case "todos":
		// Preserve non-JSON strings so the todo handler can interpret bullet lists.
		return strValue
	}

	return strValue
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return value != ""
	}
}

func parseJSONValue(value string) (interface{}, bool) {
	if value == "" {
		return nil, false
	}

	switch value[0] {
	case '[', '{':
	default:
		return nil, false
	}

	var parsed interface{}
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return nil, false
	}
	return parsed, true
}

func splitAndTrim(value string, sep string) []interface{} {
	parts := strings.Split(value, sep)
	result := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// GetToolsPrompt returns all tool schemas (legacy, for parent execution)
// Deprecated: Use GetToolsPromptForContext instead
func GetToolsPrompt() string {
	return GetToolsPromptForContext(ExecutionContextParent)
}

// GetToolsPromptForContext returns tool schemas filtered by execution context
func GetToolsPromptForContext(ctx ExecutionContext) string {
	registryLock.RLock()
	defer registryLock.RUnlock()

	var prompt string
	prompt += "<tools>\n"

	// Sort tools by name for consistent output
	var toolNames []string
	for name := range toolRegistry {
		toolNames = append(toolNames, name)
	}
	// Simple sort for determinism
	for i := 0; i < len(toolNames)-1; i++ {
		for j := i + 1; j < len(toolNames); j++ {
			if toolNames[j] < toolNames[i] {
				toolNames[i], toolNames[j] = toolNames[j], toolNames[i]
			}
		}
	}

	// Include only tools available in this context
	for _, name := range toolNames {
		def := toolRegistry[name]
		if !def.IsAvailableInContext(ctx) {
			continue
		}
		if def.XML != "" {
			prompt += "  " + def.XML + "\n"
		}
	}
	prompt += "</tools>"
	return prompt
}

// GetAvailableTools returns a list of tool names available in the given context
func GetAvailableTools(ctx ExecutionContext) []string {
	registryLock.RLock()
	defer registryLock.RUnlock()

	var tools []string
	for name, def := range toolRegistry {
		if def.IsAvailableInContext(ctx) {
			tools = append(tools, name)
		}
	}

	// Sort for determinism
	for i := 0; i < len(tools)-1; i++ {
		for j := i + 1; j < len(tools); j++ {
			if tools[j] < tools[i] {
				tools[i], tools[j] = tools[j], tools[i]
			}
		}
	}

	return tools
}

// ValidateToolCallInContext checks if a tool can be executed in the given context
// Returns error if tool doesn't exist or isn't available in context
func ValidateToolCallInContext(toolName string, ctx ExecutionContext) error {
	registryLock.RLock()
	defer registryLock.RUnlock()

	def, exists := toolRegistry[toolName]
	if !exists {
		availableTools := make([]string, 0, len(toolRegistry))
		for name := range toolRegistry {
			availableTools = append(availableTools, name)
		}
		return fmt.Errorf("tool '%s' does not exist (available tools: %v)", toolName, availableTools)
	}

	if !def.IsAvailableInContext(ctx) {
		available := make([]string, 0)
		for name, tool := range toolRegistry {
			if tool.IsAvailableInContext(ctx) {
				available = append(available, name)
			}
		}
		return fmt.Errorf(
			"tool '%s' not available in %s context (available tools: %v)",
			toolName, ctx, available,
		)
	}

	return nil
}
