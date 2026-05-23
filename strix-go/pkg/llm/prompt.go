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
	path := filepath.Join(cwd, "strix")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	path = filepath.Join(cwd, "..", "strix")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	path = filepath.Join(cwd, "..", "..", "strix")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return "strix"
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

func CompileSystemPrompt(agentName string, skillNames []string, systemPromptContext map[string]interface{}) (string, error) {
	templatePath := filepath.Join(AgentsDir, agentName, "system_prompt.jinja")
	tpl, err := pongo2.FromFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to load system prompt template: %w", err)
	}

	ctx := pongo2.Context{
		"loaded_skill_names": skillNames,
		"get_skill": func(name string) string {
			return loadSkillContent(name)
		},
		"get_tools_prompt": func() string {
			return tools.GetToolsPrompt()
		},
		"system_prompt_context": systemPromptContext,
	}

	rendered, err := tpl.Execute(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to render system prompt template: %w", err)
	}

	return rendered, nil
}
