package notes

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/usestrix/strix-go/pkg/tools"
)

type Note struct {
	NoteID       string   `json:"note_id"`
	Title        string   `json:"title"`
	Content      string   `json:"content"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	WikiFilename string   `json:"wiki_filename,omitempty"`
}

type NoteEvent struct {
	Timestamp string `json:"timestamp"`
	Op        string `json:"op"`
	NoteID    string `json:"note_id"`
	Note      *Note  `json:"note,omitempty"`
}

var (
	RunDir          string
	loadedRunDir    string
	notesStorage    = make(map[string]*Note)
	notesLock       sync.RWMutex
	validCategories = map[string]bool{
		"general":     true,
		"findings":    true,
		"methodology": true,
		"questions":   true,
		"plan":        true,
		"wiki":        true,
	}
)

func RegisterNotesTools() {
	tools.Register("create_note", false, CreateNote)
	tools.Register("list_notes", false, ListNotes)
	tools.Register("get_note", false, GetNote)
	tools.Register("update_note", false, UpdateNote)
	tools.Register("delete_note", false, DeleteNote)
}

func getNotesJSONLPath() string {
	if RunDir == "" {
		return ""
	}
	notesDir := filepath.Join(RunDir, "notes")
	_ = os.MkdirAll(notesDir, 0755)
	return filepath.Join(notesDir, "notes.jsonl")
}

func getWikiDirectory() string {
	if RunDir == "" {
		return ""
	}
	wikiDir := filepath.Join(RunDir, "wiki")
	_ = os.MkdirAll(wikiDir, 0755)
	return wikiDir
}

func sanitizeWikiTitle(title string) string {
	reg := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	cleaned := reg.ReplaceAllString(strings.ToLower(title), "-")
	slug := strings.Trim(cleaned, "-")
	if slug == "" {
		return "wiki-note"
	}
	return slug
}

func getWikiNotePath(noteID string, note *Note) string {
	wikiDir := getWikiDirectory()
	if wikiDir == "" {
		return ""
	}

	if note.WikiFilename == "" {
		note.WikiFilename = fmt.Sprintf("%s-%s.md", noteID, sanitizeWikiTitle(note.Title))
	}

	return filepath.Join(wikiDir, note.WikiFilename)
}

func persistWikiNote(noteID string, note *Note) {
	wikiPath := getWikiNotePath(noteID, note)
	if wikiPath == "" {
		return
	}

	tagsLine := "none"
	if len(note.Tags) > 0 {
		tagsLine = strings.Join(note.Tags, ", ")
	}

	content := fmt.Sprintf("# %s\n\n**Note ID:** %s\n**Created:** %s\n**Updated:** %s\n**Tags:** %s\n\n## Content\n\n%s\n",
		note.Title, noteID, note.CreatedAt, note.UpdatedAt, tagsLine, note.Content)

	_ = os.WriteFile(wikiPath, []byte(content), 0644)
}

func removeWikiNote(noteID string, note *Note) {
	wikiPath := getWikiNotePath(noteID, note)
	if wikiPath != "" {
		_ = os.Remove(wikiPath)
	}
}

func appendNoteEvent(op string, noteID string, note *Note) {
	jsonlPath := getNotesJSONLPath()
	if jsonlPath == "" {
		return
	}

	event := NoteEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Op:        op,
		NoteID:    noteID,
		Note:      note,
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

func ensureNotesLoaded() {
	if RunDir == "" {
		return
	}

	currentRunDir := filepath.Clean(RunDir)
	if loadedRunDir == currentRunDir {
		return
	}

	notesStorage = make(map[string]*Note)
	jsonlPath := getNotesJSONLPath()
	if jsonlPath == "" {
		return
	}

	slog.Info("Notes system: loading notes database", slog.String("path", jsonlPath))
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

		var event NoteEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		op := strings.ToLower(event.Op)
		if op == "delete" {
			delete(notesStorage, event.NoteID)
			continue
		}

		if event.Note == nil {
			continue
		}

		existing, exists := notesStorage[event.NoteID]
		if exists {
			// Update existing fields
			if event.Note.Title != "" {
				existing.Title = event.Note.Title
			}
			if event.Note.Content != "" {
				existing.Content = event.Note.Content
			}
			if event.Note.Category != "" {
				existing.Category = event.Note.Category
			}
			if len(event.Note.Tags) > 0 {
				existing.Tags = event.Note.Tags
			}
			existing.UpdatedAt = event.Note.UpdatedAt
			if event.Note.WikiFilename != "" {
				existing.WikiFilename = event.Note.WikiFilename
			}
		} else {
			notesStorage[event.NoteID] = event.Note
		}
	}

	// Persist wiki notes locally if any
	for noteID, note := range notesStorage {
		if note.Category == "wiki" {
			persistWikiNote(noteID, note)
		}
	}

	slog.Info("Notes system: database loaded successfully", slog.Int("notes_count", len(notesStorage)))
	loadedRunDir = currentRunDir
}

// Tool handlers wrapping operations for the tools registry

func CreateNote(args map[string]interface{}) (interface{}, error) {
	notesLock.Lock()
	defer notesLock.Unlock()

	ensureNotesLoaded()

	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	category, _ := args["category"].(string)
	if category == "" {
		category = "general"
	}

	var tags []string
	if rawTags, ok := args["tags"].([]interface{}); ok {
		for _, t := range rawTags {
			if str, ok := t.(string); ok {
				tags = append(tags, str)
			}
		}
	}

	if strings.TrimSpace(title) == "" {
		return map[string]interface{}{"success": false, "error": "Title cannot be empty"}, nil
	}
	if strings.TrimSpace(content) == "" {
		return map[string]interface{}{"success": false, "error": "Content cannot be empty"}, nil
	}
	if !validCategories[category] {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("Invalid category: %s", category)}, nil
	}

	noteID := fmt.Sprintf("note_%d", time.Now().UnixNano()/1e6%1000000)
	timestamp := time.Now().UTC().Format(time.RFC3339)

	newNote := &Note{
		NoteID:    noteID,
		Title:     title,
		Content:   content,
		Category:  category,
		Tags:      tags,
		CreatedAt: timestamp,
		UpdatedAt: timestamp,
	}

	notesStorage[noteID] = newNote
	appendNoteEvent("create", noteID, newNote)

	slog.Info("Notes system: created note",
		slog.String("note_id", noteID),
		slog.String("title", title),
		slog.String("category", category),
	)

	if category == "wiki" {
		persistWikiNote(noteID, newNote)
	}

	return map[string]interface{}{
		"success": true,
		"note_id": noteID,
		"message": fmt.Sprintf("Note '%s' created successfully", title),
	}, nil
}

func ListNotes(args map[string]interface{}) (interface{}, error) {
	notesLock.RLock()
	defer notesLock.RUnlock()

	ensureNotesLoaded()

	category, _ := args["category"].(string)
	search, _ := args["search"].(string)
	includeContent, _ := args["include_content"].(bool)

	var filterTags []string
	if rawTags, ok := args["tags"].([]interface{}); ok {
		for _, t := range rawTags {
			if str, ok := t.(string); ok {
				filterTags = append(filterTags, str)
			}
		}
	}

	var notesList []map[string]interface{}
	for noteID, note := range notesStorage {
		if category != "" && note.Category != category {
			continue
		}

		if len(filterTags) > 0 {
			tagMatch := false
			for _, ft := range filterTags {
				for _, nt := range note.Tags {
					if ft == nt {
						tagMatch = true
						break
					}
				}
			}
			if !tagMatch {
				continue
			}
		}

		if search != "" {
			searchLower := strings.ToLower(search)
			titleMatch := strings.Contains(strings.ToLower(note.Title), searchLower)
			contentMatch := strings.Contains(strings.ToLower(note.Content), searchLower)
			if !titleMatch && !contentMatch {
				continue
			}
		}

		entry := map[string]interface{}{
			"note_id":    noteID,
			"title":      note.Title,
			"category":   note.Category,
			"tags":       note.Tags,
			"created_at": note.CreatedAt,
			"updated_at": note.UpdatedAt,
		}

		if note.WikiFilename != "" {
			entry["wiki_filename"] = note.WikiFilename
		}

		if includeContent {
			entry["content"] = note.Content
		} else {
			// Provide short preview
			if len(note.Content) > 280 {
				entry["content_preview"] = note.Content[:280] + "..."
			} else {
				entry["content_preview"] = note.Content
			}
		}

		notesList = append(notesList, entry)
	}

	return map[string]interface{}{
		"success":     true,
		"notes":       notesList,
		"total_count": len(notesList),
	}, nil
}

func GetNote(args map[string]interface{}) (interface{}, error) {
	notesLock.RLock()
	defer notesLock.RUnlock()

	ensureNotesLoaded()

	noteID, _ := args["note_id"].(string)
	if strings.TrimSpace(noteID) == "" {
		return map[string]interface{}{"success": false, "error": "Note ID cannot be empty"}, nil
	}

	note, exists := notesStorage[noteID]
	if !exists {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("Note with ID '%s' not found", noteID)}, nil
	}

	return map[string]interface{}{
		"success": true,
		"note":    note,
	}, nil
}

func UpdateNote(args map[string]interface{}) (interface{}, error) {
	notesLock.Lock()
	defer notesLock.Unlock()

	ensureNotesLoaded()

	noteID, _ := args["note_id"].(string)
	if strings.TrimSpace(noteID) == "" {
		return map[string]interface{}{"success": false, "error": "Note ID cannot be empty"}, nil
	}

	note, exists := notesStorage[noteID]
	if !exists {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("Note with ID '%s' not found", noteID)}, nil
	}

	title, hasTitle := args["title"].(string)
	content, hasContent := args["content"].(string)

	if hasTitle {
		if strings.TrimSpace(title) == "" {
			return map[string]interface{}{"success": false, "error": "Title cannot be empty"}, nil
		}
		note.Title = title
	}

	if hasContent {
		if strings.TrimSpace(content) == "" {
			return map[string]interface{}{"success": false, "error": "Content cannot be empty"}, nil
		}
		note.Content = content
	}

	if rawTags, ok := args["tags"].([]interface{}); ok {
		var tags []string
		for _, t := range rawTags {
			if str, ok := t.(string); ok {
				tags = append(tags, str)
			}
		}
		note.Tags = tags
	}

	note.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	appendNoteEvent("update", noteID, note)

	slog.Info("Notes system: updated note",
		slog.String("note_id", noteID),
		slog.String("title", note.Title),
	)

	if note.Category == "wiki" {
		persistWikiNote(noteID, note)
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Note '%s' updated successfully", note.Title),
	}, nil
}

func DeleteNote(args map[string]interface{}) (interface{}, error) {
	notesLock.Lock()
	defer notesLock.Unlock()

	ensureNotesLoaded()

	noteID, _ := args["note_id"].(string)
	if strings.TrimSpace(noteID) == "" {
		return map[string]interface{}{"success": false, "error": "Note ID cannot be empty"}, nil
	}

	note, exists := notesStorage[noteID]
	if !exists {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("Note with ID '%s' not found", noteID)}, nil
	}

	if note.Category == "wiki" {
		removeWikiNote(noteID, note)
	}

	delete(notesStorage, noteID)
	appendNoteEvent("delete", noteID, nil)

	slog.Info("Notes system: deleted note",
		slog.String("note_id", noteID),
		slog.String("title", note.Title),
	)

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Note '%s' deleted successfully", note.Title),
	}, nil
}

func GetNotesList() []*Note {
	notesLock.RLock()
	defer notesLock.RUnlock()
	ensureNotesLoaded()
	var list []*Note
	for _, note := range notesStorage {
		list = append(list, note)
	}
	return list
}
