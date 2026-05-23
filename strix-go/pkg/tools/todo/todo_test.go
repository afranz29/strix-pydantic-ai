package todo

import "testing"

func TestCreateTodoDefaultsPriorityWithoutPanicking(t *testing.T) {
	todosStorage = make(map[string]map[string]*Todo)

	result, err := CreateTodo(map[string]interface{}{
		"agent_id": "agent_root",
		"title":    "Confirm default priority",
	})
	if err != nil {
		t.Fatalf("CreateTodo returned error: %v", err)
	}

	response, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if success, _ := response["success"].(bool); !success {
		t.Fatalf("CreateTodo reported failure: %#v", response)
	}

	agentTodos := todosStorage["agent_root"]
	if len(agentTodos) != 1 {
		t.Fatalf("expected one todo, got %d", len(agentTodos))
	}
	for _, todo := range agentTodos {
		if todo.Priority != "normal" {
			t.Fatalf("expected default priority normal, got %q", todo.Priority)
		}
	}
}
