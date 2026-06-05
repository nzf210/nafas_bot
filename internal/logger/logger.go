// ============================================================
// MODULE: logger
// Deskripsi: Structured logging dengan output ke stdout dan database
// ============================================================

package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Nama Function: New
// Deskripsi: Membuat instance logger baru dengan konfigurasi.
// Parameter/Value Input:
//   - level: string — level log (debug, info, warn, error)
//   - module: string — nama module untuk filtering
//   - writer: io.Writer — output writer (default stdout)
// Function yang Dipanggil/Dikonsumsi:
//   - os.Stdout: dipanggil sebagai default writer
//   - json.Marshal: dipanggil untuk serialize metadata
// Output/Return Value:
//   - *Logger: pointer ke instance logger
func New(level, module string, writer io.Writer) *Logger {
	if writer == nil {
		writer = os.Stdout
	}
	return &Logger{
		level:   level,
		module:  module,
		writer:  writer,
		start:   time.Now(),
		fields:  make(map[string]interface{}),
	}
}

// Logger structured logger
type Logger struct {
	level   string
	module  string
	writer  io.Writer
	start   time.Time
	fields  map[string]interface{}
	ctx     context.Context
}

// Nama Function: WithContext
// Deskripsi: Membuat logger baru dengan context yang disisipkan.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk logging
// Output/Return Value:
//   - *Logger: logger baru dengan context
func (l *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{
		level:  l.level,
		module: l.module,
		writer: l.writer,
		start:  l.start,
		fields: l.fields,
		ctx:    ctx,
	}
}

// Nama Function: WithField
// Deskripsi: Menambahkan field ke logger (chainable).
// Parameter/Value Input:
//   - key: string — nama field
//   - value: interface{} — nilai field
// Output/Return Value:
//   - *Logger: logger baru dengan field tambahan
func (l *Logger) WithField(key string, value interface{}) *Logger {
	fields := make(map[string]interface{})
	for k, v := range l.fields {
		fields[k] = v
	}
	fields[key] = value
	return &Logger{
		level:  l.level,
		module: l.module,
		writer: l.writer,
		start:  l.start,
		fields: fields,
		ctx:    l.ctx,
	}
}

// Nama Function: WithFields
// Deskripsi: Menambahkan multiple fields ke logger (chainable).
// Parameter/Value Input:
//   - fields: map[string]interface{} — map fields yang ditambahkan
// Output/Return Value:
//   - *Logger: logger baru dengan fields tambahan
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}
	return &Logger{
		level:  l.level,
		module: l.module,
		writer: l.writer,
		start:  l.start,
		fields: newFields,
		ctx:    l.ctx,
	}
}

// Nama Function: WithError
// Deskripsi: Menambahkan error ke logger fields.
// Parameter/Value Input:
//   - err: error — error yang dilog
// Output/Return Value:
//   - *Logger: logger baru dengan error field
func (l *Logger) WithError(err error) *Logger {
	return l.WithField("error", err.Error())
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Module    string                 `json:"module"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// Nama Function: log
// Deskripsi: Internal function untuk output log entry.
// Parameter/Value Input:
//   - level: string — level log
//   - msg: string — pesan log
//   - fields: map[string]interface{} — fields tambahan
// Function yang Dipanggil/Dikonsumsi:
//   - json.Marshal: dipanggil untuk serialize log entry
//   - io.WriteString: dipanggil untuk write ke output
// Output/Return Value:
//   - Tidak ada return value
func (l *Logger) log(level, msg string, fields map[string]interface{}) {
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Module:    l.module,
		Message:   msg,
		Fields:    fields,
	}
	data, _ := json.Marshal(entry)
	l.writer.Write(append(data, '\n'))
}

// Debug logs debug level message
func (l *Logger) Debug(msg string) {
	if l.shouldLog("debug") {
		l.log("DEBUG", msg, l.fields)
	}
}

// Info logs info level message
func (l *Logger) Info(msg string) {
	if l.shouldLog("info") {
		l.log("INFO", msg, l.fields)
	}
}

// Warn logs warn level message
func (l *Logger) Warn(msg string) {
	if l.shouldLog("warn") {
		l.log("WARN", msg, l.fields)
	}
}

// Error logs error level message
func (l *Logger) Error(msg string) {
	if l.shouldLog("error") {
		l.log("ERROR", msg, l.fields)
	}
}

// Debugf logs debug with formatting
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.Debug(fmt.Sprintf(format, args...))
}

// Infof logs info with formatting
func (l *Logger) Infof(format string, args ...interface{}) {
	l.Info(fmt.Sprintf(format, args...))
}

// Warnf logs warn with formatting
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.Warn(fmt.Sprintf(format, args...))
}

// Errorf logs error with formatting
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.Error(fmt.Sprintf(format, args...))
}

// Fatalf logs fatal message and exits
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}

func (l *Logger) shouldLog(level string) bool {
	levels := map[string]int{
		"debug": 0,
		"info":  1,
		"warn":  2,
		"error": 3,
	}
	current, ok := levels[l.level]
	if !ok {
		current = 1
	}
	target, ok := levels[level]
	if !ok {
		target = 1
	}
	return target >= current
}

// Global logger instance — thread-safe via sync.Once
var (
	globalLogger *Logger
	globalOnce   sync.Once
)

// Init initializes global logger
func Init(level, module string) {
	globalOnce.Do(func() {
		globalLogger = New(level, module, os.Stdout)
	})
}

// Default returns global logger
func Default() *Logger {
	globalOnce.Do(func() {
		globalLogger = New("info", "app", os.Stdout)
	})
	return globalLogger
}