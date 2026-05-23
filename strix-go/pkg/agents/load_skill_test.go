package agents

import (
	"testing"
)

func TestLoadSkill(t *testing.T) {
	// Create a mock Agent
	agent := &Agent{
		ID:     "test_agent_123",
		Name:   "Test Agent",
		Skills: []string{"coordination/root_agent"},
	}

	RegisterActiveAgent(agent)
	defer DeregisterActiveAgent(agent.ID)

	// Test case 1: Successful skill load (recursive and direct matches)
	args := map[string]interface{}{
		"agent_id": "test_agent_123",
		"skills":   "nmap,tooling/httpx",
	}

	res, err := LoadSkill(args)
	if err != nil {
		t.Fatalf("LoadSkill failed: %v", err)
	}

	respMap, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map response, got %T", res)
	}

	if success, _ := respMap["success"].(bool); !success {
		t.Errorf("expected success=true, got response: %+v", respMap)
	}

	// Verify the agent skills were updated (nmap is matched recursively, tooling/httpx directly)
	expectedSkills := []string{"coordination/root_agent", "nmap", "tooling/httpx"}
	if len(agent.Skills) != len(expectedSkills) {
		t.Fatalf("expected %d skills, got %v", len(expectedSkills), agent.Skills)
	}
	for i, s := range agent.Skills {
		if s != expectedSkills[i] {
			t.Errorf("expected skill %d to be %q, got %q", i, expectedSkills[i], s)
		}
	}

	// Test case 2: Load duplicate skill (should deduplicate)
	argsDup := map[string]interface{}{
		"agent_id": "test_agent_123",
		"skills":   "nmap,coordination/root_agent",
	}
	resDup, err := LoadSkill(argsDup)
	if err != nil {
		t.Fatalf("LoadSkill for duplicate failed: %v", err)
	}
	respDupMap, _ := resDup.(map[string]interface{})
	if success, _ := respDupMap["success"].(bool); !success {
		t.Errorf("expected success=true for duplicate load, got %+v", respDupMap)
	}
	
	// Skills should still be: coordination/standard, nmap, tooling/httpx (no duplicate added)
	if len(agent.Skills) != len(expectedSkills) {
		t.Errorf("expected still %d skills after dup load, got %v", len(expectedSkills), agent.Skills)
	}

	// Test case 3: Skill not found
	argsInvalid := map[string]interface{}{
		"agent_id": "test_agent_123",
		"skills":   "non_existent_skill_xyz",
	}
	resInvalid, err := LoadSkill(argsInvalid)
	if err != nil {
		t.Fatalf("LoadSkill invalid failed: %v", err)
	}
	respInvalidMap, _ := resInvalid.(map[string]interface{})
	if success, _ := respInvalidMap["success"].(bool); success {
		t.Errorf("expected success=false for non-existent skill, got response %+v", respInvalidMap)
	}
}
