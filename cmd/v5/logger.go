package main

import (
	"encoding/json"
	"os"
	"sync"
)

type ResultLogger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

func NewResultLogger(path string) (*ResultLogger, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(file)
	enc.SetEscapeHTML(false)
	return &ResultLogger{file: file, enc: enc}, nil
}

func (logger *ResultLogger) Write(result ProbeResult) error {
	if logger == nil {
		return nil
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return logger.enc.Encode(result)
}

func (logger *ResultLogger) Close() error {
	if logger == nil || logger.file == nil {
		return nil
	}
	return logger.file.Close()
}
