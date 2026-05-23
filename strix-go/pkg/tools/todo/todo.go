package todo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/usestrix/strix-go/pkg/tools"
)

type Todo struct {
	TodoID      string `json:"todo_id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

var (
	todosLock    sync.RWMutex
	todosStorage = make(map[string]map[string]*Todo) // agent_id -> (todo_id -> todo)
)

func RegisterTodoTools() {
	tools.Register("create_todo", false, CreateTodo)
	tools.Register("list_todos", false, ListTodos)
	tools.Register("update_todo", false, UpdateTodo)
	tools.Register("mark_todo_done", false, MarkTodoDone)
	tools.Register("mark_todo_pending", false, MarkTodoPending)
	tools.Register("delete_todo", false, DeleteTodo)
}

func getAgentTodos(agentID string) map[string]*Todo {
	if _, exists := todosStorage[agentID]; !exists {
		todosStorage[agentID] = make(map[string]*Todo)
	}
	return todosStorage[agentID]
}

func generateTodoID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func validatePriority(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	switch p {
	case "low", "high", "critical":
		return p
	default:
		return "normal"
	}
}

func validateStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "in_progress", "done":
		return s
	default:
		return "pending"
	}
}

func CreateTodo(args map[string]interface{}) (interface{}, error) {
	todosLock.Lock()
	defer todosLock.Unlock()

	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		agentID = "global"
	}

	agentTodos := getAgentTodos(agentID)

	title, _ := args["title"].(string)
	desc, _ := args["description"].(string)
	priority := validatePriority(args["priority"].(string))

	var createdTodos []*Todo

	// Check for bulk creation in 'todos' parameter
	if rawTodos, exists := args["todos"]; exists && rawTodos != nil {
		var bulkList []interface{}
		switch val := rawTodos.(type) {
		case string:
			if strings.TrimSpace(val) != "" {
				_ = json.Unmarshal([]byte(val), &bulkList)
			}
		case []interface{}:
			bulkList = val
		}

		for _, item := range bulkList {
			if m, ok := item.(map[string]interface{}); ok {
				tTitle, _ := m["title"].(string)
				if strings.TrimSpace(tTitle) == "" {
					continue
				}
				tDesc, _ := m["description"].(string)
				tPriority, _ := m["priority"].(string)

				todoID := generateTodoID()
				timestamp := time.Now().UTC().Format(time.RFC3339)
				todo := &Todo{
					TodoID:      todoID,
					Title:       tTitle,
					Description: tDesc,
					Priority:    validatePriority(tPriority),
					Status:      "pending",
					CreatedAt:   timestamp,
					UpdatedAt:   timestamp,
				}
				agentTodos[todoID] = todo
				createdTodos = append(createdTodos, todo)
			}
		}
	}

	// Create single todo if title is provided
	if strings.TrimSpace(title) != "" {
		todoID := generateTodoID()
		timestamp := time.Now().UTC().Format(time.RFC3339)
		todo := &Todo{
			TodoID:      todoID,
			Title:       title,
			Description: desc,
			Priority:    priority,
			Status:      "pending",
			CreatedAt:   timestamp,
			UpdatedAt:   timestamp,
		}
		agentTodos[todoID] = todo
		createdTodos = append(createdTodos, todo)
	}

	if len(createdTodos) == 0 {
		return map[string]interface{}{"success": false, "error": "Provide a title or 'todos' list to create"}, nil
	}

	slog.Info("Todo system: created todo task(s)", slog.String("agent_id", agentID), slog.Int("count", len(createdTodos)))

	return map[string]interface{}{
		"success":       true,
		"created_count": len(createdTodos),
		"todos":         createdTodos,
	}, nil
}

func ListTodos(args map[string]interface{}) (interface{}, error) {
	todosLock.RLock()
	defer todosLock.RUnlock()

	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		agentID = "global"
	}

	agentTodos := getAgentTodos(agentID)

	statusFilter, _ := args["status"].(string)
	priorityFilter, _ := args["priority"].(string)

	var list []*Todo
	for _, todo := range agentTodos {
		if statusFilter != "" && todo.Status != statusFilter {
			continue
		}
		if priorityFilter != "" && todo.Priority != priorityFilter {
			continue
		}
		list = append(list, todo)
	}

	return map[string]interface{}{
		"success":     true,
		"todos":       list,
		"total_count": len(list),
	}, nil
}

func UpdateTodo(args map[string]interface{}) (interface{}, error) {
	todosLock.Lock()
	defer todosLock.Unlock()

	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		agentID = "global"
	}

	agentTodos := getAgentTodos(agentID)

	todoID, _ := args["todo_id"].(string)
	if todoID == "" {
		return map[string]interface{}{"success": false, "error": "todo_id parameter is required"}, nil
	}

	todo, exists := agentTodos[todoID]
	if !exists {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("Todo with ID '%s' not found", todoID)}, nil
	}

	title, hasTitle := args["title"].(string)
	desc, hasDesc := args["description"].(string)
	priority, hasPriority := args["priority"].(string)
	status, hasStatus := args["status"].(string)

	if hasTitle {
		todo.Title = title
	}
	if hasDesc {
		todo.Description = desc
	}
	if hasPriority {
		todo.Priority = validatePriority(priority)
	}
	if hasStatus {
		todo.Status = validateStatus(status)
	}

	todo.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	slog.Info("Todo system: updated todo task", slog.String("todo_id", todoID), slog.String("title", todo.Title))

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Todo '%s' updated successfully", todo.Title),
		"todo":    todo,
	}, nil
}

func MarkTodoDone(args map[string]interface{}) (interface{}, error) {
	todosLock.Lock()
	defer todosLock.Unlock()

	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		agentID = "global"
	}

	agentTodos := getAgentTodos(agentID)

	var ids []string
	if todoID, _ := args["todo_id"].(string); todoID != "" {
		ids = append(ids, todoID)
	}

	// Support slice of ids
	if rawIDs, ok := args["todo_ids"].([]interface{}); ok {
		for _, v := range rawIDs {
			if id, ok := v.(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}

	updatedCount := 0
	for _, id := range ids {
		if todo, exists := agentTodos[id]; exists {
			todo.Status = "done"
			todo.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			updatedCount++
		}
	}

	slog.Info("Todo system: completed todo task(s)", slog.String("agent_id", agentID), slog.Int("count", updatedCount))

	return map[string]interface{}{
		"success":       true,
		"updated_count": updatedCount,
		"message":       fmt.Sprintf("Marked %d todo(s) as completed", updatedCount),
	}, nil
}

func MarkTodoPending(args map[string]interface{}) (interface{}, error) {
	todosLock.Lock()
	defer todosLock.Unlock()

	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		agentID = "global"
	}

	agentTodos := getAgentTodos(agentID)

	var ids []string
	if todoID, _ := args["todo_id"].(string); todoID != "" {
		ids = append(ids, todoID)
	}

	if rawIDs, ok := args["todo_ids"].([]interface{}); ok {
		for _, v := range rawIDs {
			if id, ok := v.(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}

	updatedCount := 0
	for _, id := range ids {
		if todo, exists := agentTodos[id]; exists {
			todo.Status = "pending"
			todo.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			updatedCount++
		}
	}

	slog.Info("Todo system: marked todo task(s) as pending", slog.String("agent_id", agentID), slog.Int("count", updatedCount))

	return map[string]interface{}{
		"success":       true,
		"updated_count": updatedCount,
		"message":       fmt.Sprintf("Marked %d todo(s) as pending", updatedCount),
	}, nil
}

func DeleteTodo(args map[string]interface{}) (interface{}, error) {
	todosLock.Lock()
	defer todosLock.Unlock()

	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		agentID = "global"
	}

	agentTodos := getAgentTodos(agentID)

	var ids []string
	if todoID, _ := args["todo_id"].(string); todoID != "" {
		ids = append(ids, todoID)
	}

	if rawIDs, ok := args["todo_ids"].([]interface{}); ok {
		for _, v := range rawIDs {
			if id, ok := v.(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}

	deletedCount := 0
	for _, id := range ids {
		if _, exists := agentTodos[id]; exists {
			delete(agentTodos, id)
			deletedCount++
		}
	}

	slog.Info("Todo system: deleted todo task(s)", slog.String("agent_id", agentID), slog.Int("count", deletedCount))

	return map[string]interface{}{
		"success":       true,
		"deleted_count": deletedCount,
		"message":       fmt.Sprintf("Deleted %d todo(s)", deletedCount),
	}, nil
}

func GetTodoList() []*Todo {
	todosLock.RLock()
	defer todosLock.RUnlock()
	var list []*Todo
	for _, agentTodos := range todosStorage {
		for _, todo := range agentTodos {
			list = append(list, todo)
		}
	}
	return list
}
