package llm

import (
	"regexp"
	"strings"
)

type ToolInvocation struct {
	ToolName string                 `json:"tool_name"`
	Kwargs   map[string]interface{} `json:"kwargs"`
}

func NormalizeToolFormat(content string) string {
	content = strings.ReplaceAll(content, "<minimax:tool_call>", "")
	content = strings.ReplaceAll(content, "</minimax:tool_call>", "")
	content = strings.ReplaceAll(content, "<function_calls>", "")
	content = strings.ReplaceAll(content, "</function_calls>", "")

	reInvoke := regexp.MustCompile(`<invoke\s+name=["']([^"']+)["']>`)
	content = reInvoke.ReplaceAllString(content, `<function=$1>`)
	content = strings.ReplaceAll(content, "</invoke>", "</function>")

	reParamName := regexp.MustCompile(`<parameter\s+name=["']([^"']+)["']>`)
	content = reParamName.ReplaceAllString(content, `<parameter=$1>`)

	reQuotes := regexp.MustCompile(`<(function|parameter)\s*=\s*["']([^"']+)["']>`)
	content = reQuotes.ReplaceAllString(content, `<$1=$2>`)

	return content
}

func ParseToolInvocations(content string) []ToolInvocation {
	content = NormalizeToolFormat(content)

	var invs []ToolInvocation

	fnRegex := regexp.MustCompile(`(?s)<function=([^>]+)>(.*?)</function>`)
	paramRegex := regexp.MustCompile(`(?s)<parameter=([^>]+)>(.*?)</parameter>`)

	fnMatches := fnRegex.FindAllStringSubmatch(content, -1)
	for _, match := range fnMatches {
		if len(match) < 3 {
			continue
		}
		toolName := strings.TrimSpace(match[1])
		paramsBlock := match[2]

		kwargs := make(map[string]interface{})
		paramMatches := paramRegex.FindAllStringSubmatch(paramsBlock, -1)
		for _, pm := range paramMatches {
			if len(pm) < 3 {
				continue
			}
			paramName := strings.TrimSpace(pm[1])
			paramVal := strings.TrimSpace(pm[2])
			kwargs[paramName] = paramVal
		}

		invs = append(invs, ToolInvocation{
			ToolName: toolName,
			Kwargs:   kwargs,
		})
	}

	return invs
}
