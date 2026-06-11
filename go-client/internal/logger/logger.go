package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func SetupLogging(logDir string) (*zap.Logger, error) {
	// Create timestamped log file name
	timestamp := time.Now().Format("20060102_150405")
	logFile := filepath.Join(logDir, fmt.Sprintf("tui_%s.log", timestamp))

	// Create file encoder config
	fileConfig := zap.NewProductionEncoderConfig()
	fileConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Create file encoder
	fileEncoder := zapcore.NewJSONEncoder(fileConfig)

	// Create file
	file, err := os.Create(logFile)
	if err != nil {
		return nil, err
	}

	// Create core that writes to file
	core := zapcore.NewCore(fileEncoder, zapcore.AddSync(file), zapcore.DebugLevel)

	// Create logger
	logger := zap.New(core)

	// Log startup
	logger.Info("TUI logging initialized", zap.String("file", logFile))

	return logger, nil
}
