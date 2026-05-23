package todo

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
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

type TodoEvent struct {
	Timestamp string `json:"timestamp"`
	Op        string `json:"op"`
	AgentID   string `json:"agent_id"`
	TodoID    string `json:"todo_id"`
	Todo      *Todo  `json:"todo,omitempty"`
}

var (
	RunDir          string
	loadedRunDir    string
	todosLock       sync.RWMutex
	todosStorage    = make(map[string]map[string]*Todo) // agent_id -> (todo_id -> todo)
)

func RegisterTodoTools() {
	tools.Register("create_todo", false, CreateTodo)
	tools.Register("list_todos", false, ListTodos)
	tools.Register("update_todo", false, UpdateTodo)
	tools.Register("mark_todo_done", false, MarkTodoDone)
	tools.Register("mark_todo_pending", false, MarkTodoPending)
	tools.Register("delete_todo", false, DeleteTodo)
}

func getTodosJSONLPath() string {
	if RunDir == "" {
		return ""
	}
	todoDir := filepath.Join(RunDir, "todo")
	_ = os.MkdirAll(todoDir, 0755)
	return filepath.Join(todoDir, "todo.jsonl")
}

func appendTodoEvent(op string, agentID string, todoID string, todo *Todo) {
	jsonlPath := getTodosJSONLPath()
	if jsonlPath == "" {
		return
	}

	event := TodoEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Op:        op,
		AgentID:   agentID,
		TodoID:    todoID,
		Todo:      todo,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	f, err := os.OpenFile(jsonlPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	_, _ = f.Write(append(data, '\n'))
}

func ensureTodosLoadedLocked() {
	if RunDir == "" {
		return
	}

	currentRunDir := filepath.Clean(RunDir)
	if loadedRunDir == currentRunDir {
		return
	}

	todosStorage = make(map[string]map[string]*Todo)
	jsonlPath := getTodosJSONLPath()
	if jsonlPath == "" {
		return
	}

	f, err := os.Open(jsonlPath)
	if err != nil {
		loadedRunDir = currentRunDir
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event TodoEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		agentID := event.AgentID
		if agentID == "" {
			agentID = "global"
		}

		if _, exists := todosStorage[agentID]; !exists {
			todosStorage[agentID] = make(map[string]*Todo)
		}

		op := strings.ToLower(event.Op)
		if op == "delete" {
			delete(todosStorage[agentID], event.TodoID)
			continue
		}

		if event.Todo == nil {
			continue
		}

		existing, exists := todosStorage[agentID][event.TodoID]
		if exists {
			if event.Todo.Title != "" {
				existing.Title = event.Todo.Title
			}
			if event.Todo.Description != "" {
				existing.Description = event.Todo.Description
			}
			if event.Todo.Priority != "" {
				existing.Priority = event.Todo.Priority
			}
			if event.Todo.Status != "" {
				existing.Status = event.Todo.Status
			}
			existing.UpdatedAt = event.Todo.UpdatedAt
		} else {
			todosStorage[agentID][event.TodoID] = event.Todo
		}
	}

	loadedRunDir = currentRunDir
}

func getAgentTodos(agentID string) map[string]*Todo {
	ensureTodosLoadedLocked()
	if _, exists := todosStorage[agentID]; !exists {
		todosStorage[agentID] = make(map[string]*Todo)
	}
	return todosStorage[agentID]
}

func getExistingAgentTodos(agentID string) map[string]*Todo {
	ensureTodosLoadedLocked()
	if todos, exists := todosStorage[agentID]; exists {
		return todos
	}
	return map[string]*Todo{}
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
	priority, _ := args["priority"].(string)
	priority = validatePriority(priority)

	var createdTodos []*Todo

	// Check for bulk creation in 'todos' parameter
	if rawTodos, exists := args["todos"]; exists && rawTodos != nil {
		for _, item := range normalizeBulkTodos(rawTodos) {
			tTitle, _ := item["title"].(string)
			if strings.TrimSpace(tTitle) == "" {
				continue
			}
			tDesc, _ := item["description"].(string)
			tPriority, _ := item["priority"].(string)

			todoID := generateTodoID()
			timestamp := time.Now().UTC().Format(time.RFC3339)
			todo := &Todo{
				TodoID:      todoID,
				Title:       strings.TrimSpace(tTitle),
				Description: strings.TrimSpace(tDesc),
				Priority:    validatePriority(tPriority),
				Status:      "pending",
				CreatedAt:   timestamp,
				UpdatedAt:   timestamp,
			}
			agentTodos[todoID] = todo
			appendTodoEvent("create", agentID, todoID, todo)
			createdTodos = append(createdTodos, todo)
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
		appendTodoEvent("create", agentID, todoID, todo)
		createdTodos = append(createdTodos, todo)
	}

	if len(createdTodos) == 0 {
		return map[string]interface{}{"success": false, "error": "Provide a title or 'todos' list to create"}, nil
	}

	slog.Info("Todo system: created todo task(s)", slog.String("agent_id", agentID), slog.Int("count", len(createdTodos)))

	return map[string]interface{}{
		"success":     true,
		"created":     summarizeTodos(createdTodos),
		"count":       len(createdTodos),
		"todos":       sortedTodos(agentTodos),
		"total_count": len(agentTodos),
	}, nil
}

func ListTodos(args map[string]interface{}) (interface{}, error) {
	todosLock.RLock()
	defer todosLock.RUnlock()

	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		agentID = "global"
	}

	agentTodos := getExistingAgentTodos(agentID)

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
	sortTodos(list)

	summary := map[string]int{
		"pending":     0,
		"in_progress": 0,
		"done":        0,
	}
	for _, todo := range list {
		summary[todo.Status]++
	}

	return map[string]interface{}{
		"success":     true,
		"todos":       list,
		"total_count": len(list),
		"summary":     summary,
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

	if rawUpdates, exists := args["updates"]; exists && rawUpdates != nil {
		updatedIDs := make([]string, 0)
		errors := make([]string, 0)
		for _, update := range normalizeBulkUpdates(rawUpdates) {
			todoID, _ := update["todo_id"].(string)
			if todoID == "" {
				continue
			}
			todo, exists := agentTodos[todoID]
			if !exists {
				errors = append(errors, fmt.Sprintf("Todo with ID '%s' not found", todoID))
				continue
			}
			if title, ok := update["title"].(string); ok {
				todo.Title = title
			}
			if desc, ok := update["description"].(string); ok {
				todo.Description = desc
			}
			if priority, ok := update["priority"].(string); ok {
				todo.Priority = validatePriority(priority)
			}
			if status, ok := update["status"].(string); ok {
				todo.Status = validateStatus(status)
			}
			todo.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			appendTodoEvent("update", agentID, todoID, todo)
			updatedIDs = append(updatedIDs, todoID)
		}

		return map[string]interface{}{
			"success":       len(errors) == 0,
			"updated":       updatedIDs,
			"updated_count": len(updatedIDs),
			"errors":        errors,
			"todos":         sortedTodos(agentTodos),
		}, nil
	}

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
	appendTodoEvent("update", agentID, todoID, todo)

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

	ids := collectTodoIDs(args)

	updatedCount := 0
	var markedDone []string
	for _, id := range ids {
		if todo, exists := agentTodos[id]; exists {
			todo.Status = "done"
			todo.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			updatedCount++
			markedDone = append(markedDone, id)
			appendTodoEvent("update", agentID, id, todo)
		}
	}

	slog.Info("Todo system: completed todo task(s)", slog.String("agent_id", agentID), slog.Int("count", updatedCount))

	return map[string]interface{}{
		"success":       true,
		"updated_count": updatedCount,
		"message":       fmt.Sprintf("Marked %d todo(s) as completed", updatedCount),
		"marked_done":   markedDone,
		"todos":         sortedTodos(agentTodos),
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

	ids := collectTodoIDs(args)

	updatedCount := 0
	var markedPending []string
	for _, id := range ids {
		if todo, exists := agentTodos[id]; exists {
			todo.Status = "pending"
			todo.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			updatedCount++
			markedPending = append(markedPending, id)
			appendTodoEvent("update", agentID, id, todo)
		}
	}

	slog.Info("Todo system: marked todo task(s) as pending", slog.String("agent_id", agentID), slog.Int("count", updatedCount))

	return map[string]interface{}{
		"success":        true,
		"updated_count":  updatedCount,
		"message":        fmt.Sprintf("Marked %d todo(s) as pending", updatedCount),
		"marked_pending": markedPending,
		"todos":          sortedTodos(agentTodos),
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

	ids := collectTodoIDs(args)

	deletedCount := 0
	var deleted []string
	for _, id := range ids {
		if _, exists := agentTodos[id]; exists {
			delete(agentTodos, id)
			deletedCount++
			deleted = append(deleted, id)
			appendTodoEvent("delete", agentID, id, nil)
		}
	}

	slog.Info("Todo system: deleted todo task(s)", slog.String("agent_id", agentID), slog.Int("count", deletedCount))

	return map[string]interface{}{
		"success":       true,
		"deleted_count": deletedCount,
		"message":       fmt.Sprintf("Deleted %d todo(s)", deletedCount),
		"deleted":       deleted,
		"todos":         sortedTodos(agentTodos),
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
	sortTodos(list)
	return list
}

func collectTodoIDs(args map[string]interface{}) []string {
	var ids []string
	if todoID, _ := args["todo_id"].(string); todoID != "" {
		ids = append(ids, todoID)
	}
	return append(ids, normalizeTodoIDs(args["todo_ids"])...)
}

func normalizeTodoIDs(raw interface{}) []string {
	switch value := raw.(type) {
	case []string:
		return append([]string{}, value...)
	case []interface{}:
		var ids []string
		for _, item := range value {
			if id, ok := item.(string); ok && strings.TrimSpace(id) != "" {
				ids = append(ids, strings.TrimSpace(id))
			}
		}
		return ids
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		if strings.Contains(trimmed, ",") {
			parts := strings.Split(trimmed, ",")
			ids := make([]string, 0, len(parts))
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					ids = append(ids, part)
				}
			}
			return ids
		}
		return []string{trimmed}
	default:
		return nil
	}
}

func normalizeBulkTodos(raw interface{}) []map[string]interface{} {
	switch value := raw.(type) {
	case []interface{}:
		var normalized []map[string]interface{}
		for _, item := range value {
			if todo, ok := item.(map[string]interface{}); ok {
				normalized = append(normalized, todo)
			}
		}
		return normalized
	case map[string]interface{}:
		return []map[string]interface{}{value}
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		var parsed []map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
		lines := strings.Split(trimmed, "\n")
		var normalized []map[string]interface{}
		for _, line := range lines {
			title := strings.TrimSpace(strings.TrimLeft(line, "-* \t"))
			if title != "" {
				normalized = append(normalized, map[string]interface{}{"title": title})
			}
		}
		return normalized
	default:
		return nil
	}
}

func normalizeBulkUpdates(raw interface{}) []map[string]interface{} {
	switch value := raw.(type) {
	case []interface{}:
		var normalized []map[string]interface{}
		for _, item := range value {
			if update, ok := item.(map[string]interface{}); ok {
				normalized = append(normalized, update)
			}
		}
		return normalized
	case map[string]interface{}:
		return []map[string]interface{}{value}
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		var parsed []map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
		var single map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &single); err == nil {
			return []map[string]interface{}{single}
		}
		return nil
	default:
		return nil
	}
}

func sortedTodos(agentTodos map[string]*Todo) []*Todo {
	list := make([]*Todo, 0, len(agentTodos))
	for _, todo := range agentTodos {
		list = append(list, todo)
	}
	sortTodos(list)
	return list
}

func sortTodos(list []*Todo) {
	priorityOrder := map[string]int{"critical": 0, "high": 1, "normal": 2, "low": 3}
	statusOrder := map[string]int{"done": 0, "in_progress": 1, "pending": 2}
	sort.Slice(list, func(i, j int) bool {
		left := list[i]
		right := list[j]
		leftStatus := statusOrder[left.Status]
		rightStatus := statusOrder[right.Status]
		if leftStatus != rightStatus {
			return leftStatus < rightStatus
		}
		leftPriority := priorityOrder[left.Priority]
		rightPriority := priorityOrder[right.Priority]
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return left.CreatedAt < right.CreatedAt
	})
}

func summarizeTodos(list []*Todo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(list))
	for _, todo := range list {
		result = append(result, map[string]interface{}{
			"todo_id":  todo.TodoID,
			"title":    todo.Title,
			"priority": todo.Priority,
		})
	}
	return result
}
