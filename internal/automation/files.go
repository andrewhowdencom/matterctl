package automation

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxPrograms = 32

// Program is one independently loaded rule file.
type Program struct {
	Filename string
	Engine   *Engine
}

func LoadFile(ctx context.Context, filename string) (*Engine, error) {
	// #nosec G304 -- rule files are explicitly selected by the local operator.
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open rule file %q: %w", filename, err)
	}
	defer func() { _ = file.Close() }()
	source, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return nil, fmt.Errorf("read rule file %q: %w", filename, err)
	}
	return Load(ctx, filename, source)
}

// LoadDirectory reads regular .star files in lexical order. A missing default
// directory means that no automations have been configured yet.
func LoadDirectory(ctx context.Context, directory string) ([]Program, error) {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read automation directory: %w", err)
	}
	programs := make([]Program, 0)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".star") {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("automation %q is not a regular file", entry.Name())
		}
		if len(programs) == maxPrograms {
			return nil, fmt.Errorf("automation directory contains more than %d programs", maxPrograms)
		}
		filename := filepath.Join(directory, entry.Name())
		engine, err := LoadFile(ctx, filename)
		if err != nil {
			return nil, err
		}
		programs = append(programs, Program{Filename: filename, Engine: engine})
	}
	return programs, nil
}
