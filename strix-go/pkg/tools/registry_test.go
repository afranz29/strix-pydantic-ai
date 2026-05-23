package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeArgumentsConvertsStructuredValues(t *testing.T) {
	def := ToolDefinition{
		Parsed: XMLTool{
			Parameters: []XMLParameter{
				{Name: "include_content", Type: "boolean"},
				{Name: "success", Type: "boolean"},
				{Name: "findings", Type: "string"},
				{Name: "todo_ids", Type: "string"},
				{Name: "title", Type: "string"},
			},
		},
	}

	args := map[string]interface{}{
		"include_content": "true",
		"success":         "false",
		"findings":        `["one", "two"]`,
		"todo_ids":        "abc123, def456",
		"title":           `{"keep":"as string"}`,
	}

	normalized := NormalizeArguments(def, args)

	if value, ok := normalized["include_content"].(bool); !ok || !value {
		t.Fatalf("include_content was not converted to true bool: %#v", normalized["include_content"])
	}
	if value, ok := normalized["success"].(bool); !ok || value {
		t.Fatalf("success was not converted to false bool: %#v", normalized["success"])
	}
	if findings, ok := normalized["findings"].([]interface{}); !ok || len(findings) != 2 {
		t.Fatalf("findings were not converted to []interface{}: %#v", normalized["findings"])
	}
	if ids, ok := normalized["todo_ids"].([]interface{}); !ok || len(ids) != 2 {
		t.Fatalf("todo_ids were not converted to []interface{}: %#v", normalized["todo_ids"])
	}
	if title, ok := normalized["title"].(string); !ok || title != `{"keep":"as string"}` {
		t.Fatalf("title should remain a plain string: %#v", normalized["title"])
	}
}

func TestLoadSchemaParsesRawInvalidXML(t *testing.T) {
	xmlData := `
<tools>
  <tool name="terminal_execute">
    <description>Execute bash command & interact with terminal. Capped at 60s & uses & for backgrounding.</description>
    <parameters>
      <parameter name="command" type="string" required="true">
        <description>The command to execute</description>
      </parameter>
      <parameter name="is_input" type="boolean" required="false">
        <description>If true, sends command to existing process</description>
      </parameter>
    </parameters>
    <examples>
      # Example with invalid characters:
      <function=terminal_execute>
      <parameter=command>ls -la</parameter>
      </function>
    </examples>
  </tool>
</tools>
`
	// Clear registry for clean test
	toolRegistry = make(map[string]ToolDefinition)

	err := LoadSchema([]byte(xmlData))
	if err != nil {
		t.Fatalf("failed to load schema: %v", err)
	}

	def, exists := GetTool("terminal_execute")
	if !exists {
		t.Fatalf("expected tool terminal_execute to exist")
	}

	if def.Name != "terminal_execute" {
		t.Errorf("expected name 'terminal_execute', got %q", def.Name)
	}

	if len(def.Parsed.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(def.Parsed.Parameters))
	} else {
		if def.Parsed.Parameters[0].Name != "command" || def.Parsed.Parameters[0].Type != "string" {
			t.Errorf("incorrect param 0: %+v", def.Parsed.Parameters[0])
		}
		if def.Parsed.Parameters[1].Name != "is_input" || def.Parsed.Parameters[1].Type != "boolean" {
			t.Errorf("incorrect param 1: %+v", def.Parsed.Parameters[1])
		}
	}

	if !strings.Contains(def.XML, `uses & for backgrounding`) {
		t.Errorf("expected XML to contain original content, got %q", def.XML)
	}
}

func TestLoadSchemaAllProductionFiles(t *testing.T) {
	// Resolve strix/tools directory relative to this package.
	candidates := []string{
		"../../../strix-python/tools",
		"../../strix-python/tools",
		"../../../strix/tools",
		"../../strix/tools",
	}
	var toolsDir string
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			toolsDir = c
			break
		}
	}
	if toolsDir == "" {
		t.Skip("production strix/tools directory not found; skipping integration check")
	}

	toolRegistry = make(map[string]ToolDefinition)

	var schemaFiles []string
	err := filepath.Walk(toolsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), "_schema.xml") {
			schemaFiles = append(schemaFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk tools dir: %v", err)
	}

	if len(schemaFiles) == 0 {
		t.Fatalf("no _schema.xml files found under %s", toolsDir)
	}

	for _, path := range schemaFiles {
		if err := LoadSchemaFile(path); err != nil {
			t.Errorf("LoadSchemaFile(%s) failed: %v", filepath.Base(path), err)
		}
	}

	// At minimum we expect the well-known tools that the agent code references.
	requiredTools := []string{
		"agent_finish",
		"terminal_execute",
		"think",
	}
	for _, name := range requiredTools {
		if _, ok := GetTool(name); !ok {
			t.Errorf("expected tool %q to be registered after loading production schemas", name)
		}
	}

	// And every registered tool should have a non-empty raw XML for prompt injection
	// and at least the name populated in Parsed metadata.
	for name, def := range toolRegistry {
		if def.XML == "" {
			t.Errorf("tool %q has empty XML", name)
		}
		if def.Parsed.Name == "" {
			t.Errorf("tool %q has empty Parsed.Name", name)
		}
	}
}

func TestEscapeLoneAmpersandsPreservesEntities(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain text", "plain text"},
		{"a & b", "a &amp; b"},
		{"already &amp; escaped", "already &amp; escaped"},
		{"mix & with &lt;tag&gt;", "mix &amp; with &lt;tag&gt;"},
		{"numeric &#42; and hex &#x2A; entities", "numeric &#42; and hex &#x2A; entities"},
		{"trailing &", "trailing &amp;"},
		{"bogus &foo without semicolon", "bogus &amp;foo without semicolon"},
		{"&unknown; gets escaped", "&amp;unknown; gets escaped"},
	}
	for _, tc := range cases {
		got := escapeLoneAmpersands(tc.in)
		if got != tc.want {
			t.Errorf("escapeLoneAmpersands(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeForXMLParsingDropsExamples(t *testing.T) {
	in := `<tool name="x">
  <description>desc</description>
  <examples>
    <function=x>
    <parameter=arg>val & more</parameter>
    </function>
  </examples>
</tool>`
	out := sanitizeForXMLParsing(in)
	if strings.Contains(out, "<function=") {
		t.Errorf("expected examples body to be stripped, got: %s", out)
	}
	if !strings.Contains(out, "<examples></examples>") {
		t.Errorf("expected empty examples element, got: %s", out)
	}
}

func TestToolDefinitionIsAvailableInContext(t *testing.T) {
	tests := []struct {
		name         string
		sandboxExec  bool
		context      ExecutionContext
		expectAvail  bool
	}{
		{"sandbox tool in sandbox context", true, ExecutionContextSandbox, true},
		{"sandbox tool in parent context", true, ExecutionContextParent, true},
		{"parent tool in sandbox context", false, ExecutionContextSandbox, false},
		{"parent tool in parent context", false, ExecutionContextParent, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def := ToolDefinition{
				Name:             "test_tool",
				SandboxExecution: tc.sandboxExec,
			}
			available := def.IsAvailableInContext(tc.context)
			if available != tc.expectAvail {
				t.Errorf("IsAvailableInContext(%s) = %v, want %v",
					tc.context, available, tc.expectAvail)
			}
		})
	}
}

func TestGetToolsPromptForContext(t *testing.T) {
	// Clear and set up test registry
	toolRegistry = make(map[string]ToolDefinition)
	toolRegistry["sandbox_tool"] = ToolDefinition{
		Name:             "sandbox_tool",
		SandboxExecution: true,
		XML:              `<tool name="sandbox_tool"><description>A sandbox tool</description></tool>`,
	}
	toolRegistry["parent_tool"] = ToolDefinition{
		Name:             "parent_tool",
		SandboxExecution: false,
		XML:              `<tool name="parent_tool"><description>A parent tool</description></tool>`,
	}

	// Test parent context includes all tools
	parentPrompt := GetToolsPromptForContext(ExecutionContextParent)
	if !strings.Contains(parentPrompt, "sandbox_tool") {
		t.Errorf("parent context should include sandbox_tool")
	}
	if !strings.Contains(parentPrompt, "parent_tool") {
		t.Errorf("parent context should include parent_tool")
	}

	// Test sandbox context only includes sandbox tools
	sandboxPrompt := GetToolsPromptForContext(ExecutionContextSandbox)
	if !strings.Contains(sandboxPrompt, "sandbox_tool") {
		t.Errorf("sandbox context should include sandbox_tool")
	}
	if strings.Contains(sandboxPrompt, "parent_tool") {
		t.Errorf("sandbox context should NOT include parent_tool")
	}
}

func TestGetAvailableTools(t *testing.T) {
	// Clear and set up test registry
	toolRegistry = make(map[string]ToolDefinition)
	toolRegistry["sandbox_tool"] = ToolDefinition{
		Name:             "sandbox_tool",
		SandboxExecution: true,
	}
	toolRegistry["parent_tool"] = ToolDefinition{
		Name:             "parent_tool",
		SandboxExecution: false,
	}

	// Test sandbox context
	sandboxTools := GetAvailableTools(ExecutionContextSandbox)
	if len(sandboxTools) != 1 || sandboxTools[0] != "sandbox_tool" {
		t.Errorf("sandbox context should only have sandbox_tool, got %v", sandboxTools)
	}

	// Test parent context
	parentTools := GetAvailableTools(ExecutionContextParent)
	if len(parentTools) != 2 {
		t.Errorf("parent context should have 2 tools, got %d: %v", len(parentTools), parentTools)
	}
}

func TestValidateToolCallInContext(t *testing.T) {
	// Clear and set up test registry
	toolRegistry = make(map[string]ToolDefinition)
	toolRegistry["sandbox_tool"] = ToolDefinition{
		Name:             "sandbox_tool",
		SandboxExecution: true,
	}
	toolRegistry["parent_tool"] = ToolDefinition{
		Name:             "parent_tool",
		SandboxExecution: false,
	}

	tests := []struct {
		name      string
		toolName  string
		context   ExecutionContext
		wantError bool
	}{
		{"sandbox tool in sandbox", "sandbox_tool", ExecutionContextSandbox, false},
		{"sandbox tool in parent", "sandbox_tool", ExecutionContextParent, false},
		{"parent tool in parent", "parent_tool", ExecutionContextParent, false},
		{"parent tool in sandbox", "parent_tool", ExecutionContextSandbox, true},
		{"nonexistent tool", "nonexistent", ExecutionContextParent, true},
		{"nonexistent tool in sandbox", "nonexistent", ExecutionContextSandbox, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateToolCallInContext(tc.toolName, tc.context)
			if (err != nil) != tc.wantError {
				t.Errorf("ValidateToolCallInContext(%q, %s) error = %v, wantError %v",
					tc.toolName, tc.context, err, tc.wantError)
			}
		})
	}
}

func TestXMLParameterNestedDescription(t *testing.T) {
	xmlData := `
<tools>
  <tool name="test_tool">
    <description>Main tool description</description>
    <parameters>
      <parameter name="param1" type="string" required="true">
        <description>Nested description tag</description>
      </parameter>
      <parameter name="param2" type="string" required="false">
        Plain chardata description
      </parameter>
    </parameters>
  </tool>
</tools>
`
	toolRegistry = make(map[string]ToolDefinition)

	err := LoadSchema([]byte(xmlData))
	if err != nil {
		t.Fatalf("failed to load schema: %v", err)
	}

	def, exists := GetTool("test_tool")
	if !exists {
		t.Fatalf("expected tool test_tool to exist")
	}

	if len(def.Parsed.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(def.Parsed.Parameters))
	}

	param1 := def.Parsed.Parameters[0]
	if param1.getDescription() != "Nested description tag" {
		t.Errorf("param1 should have nested description, got %q", param1.getDescription())
	}

	param2 := def.Parsed.Parameters[1]
	if !strings.Contains(param2.getDescription(), "Plain chardata") {
		t.Errorf("param2 should have chardata description, got %q", param2.getDescription())
	}
}

func TestLoadSchemaMalformedXMLErrorReporting(t *testing.T) {
	xmlData := `
<tools>
  <tool name="broken_tool">
    <description>Tool with broken XML</description>
    <parameters>
      <parameter name="param1" type="string">
        <unclosed>tag
      </parameter>
    </parameters>
  </tool>
</tools>
`
	toolRegistry = make(map[string]ToolDefinition)

	err := LoadSchema([]byte(xmlData))
	if err == nil {
		t.Fatalf("expected error for malformed XML")
	}

	if !strings.Contains(err.Error(), "broken_tool") {
		t.Errorf("error should mention tool name 'broken_tool', got: %v", err)
	}
}
