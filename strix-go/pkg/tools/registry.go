package tools

import (
	"encoding/xml"
	"fmt"
	"os"
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

type ToolDefinition struct {
	Name             string
	SandboxExecution bool
	Handler          interface{} // Go function for local tools
	XML              string      // Raw XML snippet for system prompt injection
	Parsed           XMLTool
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

func LoadSchema(data []byte) error {
	registryLock.Lock()
	defer registryLock.Unlock()

	// Parse XML wrapper
	var xmlTools XMLTools
	if err := xml.Unmarshal(data, &xmlTools); err != nil {
		return fmt.Errorf("failed to parse XML tools schema: %w", err)
	}

	for _, xmlTool := range xmlTools.Tools {
		// Re-marshal individual tool to string for prompt injection
		rawToolXML, err := xml.MarshalIndent(xmlTool, "  ", "  ")
		if err != nil {
			continue
		}

		def, exists := toolRegistry[xmlTool.Name]
		if exists {
			def.Parsed = xmlTool
			def.XML = string(rawToolXML)
			toolRegistry[xmlTool.Name] = def
		} else {
			toolRegistry[xmlTool.Name] = ToolDefinition{
				Name:             xmlTool.Name,
				SandboxExecution: true, // Default to sandbox execution
				XML:              string(rawToolXML),
				Parsed:           xmlTool,
			}
		}
	}

	return nil
}

func GetTool(name string) (ToolDefinition, bool) {
	registryLock.RLock()
	defer registryLock.RUnlock()
	def, exists := toolRegistry[name]
	return def, exists
}

func GetToolsPrompt() string {
	registryLock.RLock()
	defer registryLock.RUnlock()

	var prompt string
	prompt += "<tools>\n"
	for _, def := range toolRegistry {
		if def.XML != "" {
			prompt += "  " + def.XML + "\n"
		}
	}
	prompt += "</tools>"
	return prompt
}
