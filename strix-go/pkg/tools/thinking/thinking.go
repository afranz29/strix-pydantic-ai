package thinking

import (
	"fmt"
	"strings"

	"github.com/usestrix/strix-go/pkg/tools"
)

func RegisterThinkingTools() {
	tools.Register("think", false, Think)
}

func Think(args map[string]interface{}) (interface{}, error) {
	thought, _ := args["thought"].(string)
	if strings.TrimSpace(thought) == "" {
		return map[string]interface{}{
			"success": false,
			"message": "Thought cannot be empty",
		}, nil
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Thought recorded successfully with %d characters", len(strings.TrimSpace(thought))),
	}, nil
}
