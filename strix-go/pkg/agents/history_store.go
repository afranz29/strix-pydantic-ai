package agents

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	lcsqlite3 "github.com/tmc/langchaingo/memory/sqlite3"
	"github.com/tmc/langchaingo/llms"

	"github.com/usestrix/strix-go/pkg/llm"
)

const defaultHistoryDB = "strix_history.db"

// historyEnabled returns false when STRIX_NO_HISTORY=1 is set.
func historyEnabled() bool {
	return os.Getenv("STRIX_NO_HISTORY") != "1"
}

var (
	sharedDB  *sql.DB
	dbOnce    sync.Once
	dbOpenErr error
)

func openHistoryDB() (*sql.DB, error) {
	dbOnce.Do(func() {
		path := os.Getenv("STRIX_HISTORY_DB")
		if path == "" {
			path = defaultHistoryDB
		}
		// WAL mode allows concurrent readers alongside a writer.
		sharedDB, dbOpenErr = sql.Open("sqlite3", path+"?_journal_mode=WAL")
		if dbOpenErr != nil {
			return
		}
		sharedDB.SetMaxOpenConns(1)
	})
	return sharedDB, dbOpenErr
}

func sessionHistory(ctx context.Context, agentID string) (*lcsqlite3.SqliteChatMessageHistory, error) {
	db, err := openHistoryDB()
	if err != nil {
		return nil, err
	}
	h := lcsqlite3.NewSqliteChatMessageHistory(
		lcsqlite3.WithDB(db),
		lcsqlite3.WithContext(ctx),
		lcsqlite3.WithSession(agentID),
		lcsqlite3.WithOverwrite(),
		lcsqlite3.WithLimit(100000),
	)
	return h, nil
}

// loadHistory returns all persisted messages for agentID, oldest first.
// Returns nil when history persistence is disabled.
func loadHistory(ctx context.Context, agentID string) []llm.Message {
	if !historyEnabled() {
		slog.Debug("History persistence disabled",
			slog.String("agent_id", agentID),
		)
		return nil
	}
	slog.Debug("Loading history from store",
		slog.String("agent_id", agentID),
	)
	h, err := sessionHistory(ctx, agentID)
	if err != nil {
		slog.Warn("Failed to open history store for loading",
			slog.String("agent_id", agentID),
			slog.Any("error", err),
		)
		return nil
	}
	msgs, err := h.Messages(ctx)
	if err != nil {
		slog.Warn("Failed to load history from store",
			slog.String("agent_id", agentID),
			slog.Any("error", err),
		)
		return nil
	}
	result := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		var role string
		switch m.GetType() {
		case llms.ChatMessageTypeAI:
			role = "assistant"
		case llms.ChatMessageTypeHuman:
			role = "user"
		case llms.ChatMessageTypeSystem:
			role = "system"
		default:
			continue
		}
		result = append(result, llm.Message{Role: role, Content: m.GetContent()})
	}
	slog.Info("Successfully loaded history from store",
		slog.String("agent_id", agentID),
		slog.Int("message_count", len(result)),
	)
	return result
}

// persistMessage appends a single message to the sqlite store for agentID.
// Errors are logged but not returned — a persistence failure must not stop execution.
// Does nothing when history persistence is disabled.
func persistMessage(ctx context.Context, agentID string, msg llm.Message) {
	if !historyEnabled() {
		slog.Debug("History persistence disabled, skipping message save",
			slog.String("agent_id", agentID),
			slog.String("role", msg.Role),
		)
		return
	}
	slog.Debug("Persisting message to history store",
		slog.String("agent_id", agentID),
		slog.String("role", msg.Role),
	)
	h, err := sessionHistory(ctx, agentID)
	if err != nil {
		slog.Warn("Failed to open history store for writing",
			slog.String("agent_id", agentID),
			slog.Any("error", err),
		)
		return
	}
	switch msg.Role {
	case "assistant":
		err = h.AddAIMessage(ctx, msg.Content)
	case "user":
		err = h.AddUserMessage(ctx, msg.Content)
	case "system":
		err = h.AddMessage(ctx, llms.SystemChatMessage{Content: msg.Content})
	}
	if err != nil {
		slog.Warn("Failed to persist message to history store",
			slog.String("agent_id", agentID),
			slog.String("role", msg.Role),
			slog.Any("error", err),
		)
		return
	}
	slog.Info("Successfully persisted message to history store",
		slog.String("agent_id", agentID),
		slog.String("role", msg.Role),
	)
}
