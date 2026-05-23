package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flosch/pongo2/v6"
	"github.com/usestrix/strix-go/pkg/tools"
)

var (
	AgentsDir string
	SkillsDir string
)

func init() {
	strixDir := resolveStrixDir()
	AgentsDir = filepath.Join(strixDir, "agents")
	SkillsDir = filepath.Join(strixDir, "skills")
}

func resolveStrixDir() string {
	cwd, _ := os.Getwd()
	candidates := []string{
		filepath.Join(cwd, "strix-python"),
		filepath.Join(cwd, "strix"),
		filepath.Join(cwd, "..", "strix-python"),
		filepath.Join(cwd, "..", "strix"),
		filepath.Join(cwd, "..", "..", "strix-python"),
		filepath.Join(cwd, "..", "..", "strix"),
		filepath.Join(cwd, "..", "..", "..", "strix-python"),
		filepath.Join(cwd, "..", "..", "..", "strix"),
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			toolsPath := filepath.Join(path, "tools")
			if tInfo, err := os.Stat(toolsPath); err == nil && tInfo.IsDir() {
				return path
			}
		}
	}
	return "strix-python"
}

// FindSkillFile walks the skills directory to find a matching skill markdown file
func FindSkillFile(skillName string) (string, error) {
	// If skillName contains a slash (e.g. category/name), build direct path
	if strings.Contains(skillName, "/") {
		path := filepath.Join(SkillsDir, skillName+".md")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Otherwise, search recursively
	var foundPath string
	err := filepath.Walk(SkillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == skillName+".md" {
			foundPath = path
			return filepath.SkipAll
		}
		return nil
	})

	if err != nil {
		return "", err
	}
	if foundPath == "" {
		return "", fmt.Errorf("skill %s not found in %s", skillName, SkillsDir)
	}
	return foundPath, nil
}

func loadSkillContent(skillName string) string {
	path, err := FindSkillFile(skillName)
	if err != nil {
		return fmt.Sprintf("Error: skill %s could not be loaded: %v", skillName, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("Error: failed to read skill %s: %v", skillName, err)
	}
	return string(data)
}

func orderedSkills(skillNames []string, scanMode string) []string {
	ordered := append([]string{}, skillNames...)
	if scanMode != "" {
		ordered = append(ordered, filepath.ToSlash(filepath.Join("scan_modes", scanMode)))
	}

	var deduped []string
	seen := make(map[string]struct{}, len(ordered))
	for _, skillName := range ordered {
		if skillName == "" {
			continue
		}
		if _, exists := seen[skillName]; exists {
			continue
		}
		seen[skillName] = struct{}{}
		deduped = append(deduped, skillName)
	}

	return deduped
}

// CompileSystemPrompt builds the system prompt for an agent
// Deprecated: Use CompileSystemPromptWithContext instead
func CompileSystemPrompt(
	agentName string,
	skillNames []string,
	scanMode string,
	interactive bool,
	systemPromptContext map[string]interface{},
) (string, error) {
	return CompileSystemPromptWithContext(
		agentName,
		skillNames,
		scanMode,
		interactive,
		systemPromptContext,
		tools.ExecutionContextParent,
	)
}

// CompileSystemPromptWithContext builds the system prompt for an agent with context-aware tools
func CompileSystemPromptWithContext(
	agentName string,
	skillNames []string,
	scanMode string,
	interactive bool,
	systemPromptContext map[string]interface{},
	executionContext tools.ExecutionContext,
) (string, error) {
	templatePath := filepath.Join(AgentsDir, agentName, "system_prompt.jinja")
	tpl, err := pongo2.FromFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to load system prompt template: %w", err)
	}

	loadedSkillNames := orderedSkills(skillNames, scanMode)
	ctx := pongo2.Context{
		"interactive":        interactive,
		"loaded_skill_names": loadedSkillNames,
		"get_skill": func(name string) string {
			return loadSkillContent(name)
		},
		"get_tools_prompt": func() string {
			return tools.GetToolsPromptForContext(executionContext)
		},
		"available_tools": tools.GetAvailableTools(executionContext),
		"execution_context": string(executionContext),
		"system_prompt_context": systemPromptContext,
	}

	rendered, err := tpl.Execute(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to render system prompt template: %w", err)
	}

	return rendered, nil
}
