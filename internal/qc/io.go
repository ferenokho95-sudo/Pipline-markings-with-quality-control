package qc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReadJSONL rejects unknown fields so typos cannot silently corrupt an experiment.
func ReadJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var rows []T
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var row T
		dec := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&row); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		var extra any
		if err = dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("%s line %d: expected one JSON object", path, line)
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}

// atomicWrite never leaves a partially written report after an API or disk failure.
func atomicWrite(path string, write func(io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".labelqc-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = write(f); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Windows cannot atomically replace an existing target with os.Rename.
	// Refuse overwrites for all systems: callers use a new path or remove an old file explicitly.
	if _, err = os.Stat(path); err == nil {
		return fmt.Errorf("output exists: %s (choose a new path)", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(name, path)
}
func WriteJSONL[T any](path string, rows []T) error {
	return atomicWrite(path, func(w io.Writer) error {
		e := json.NewEncoder(w)
		e.SetEscapeHTML(false)
		for _, r := range rows {
			if err := e.Encode(r); err != nil {
				return err
			}
		}
		return nil
	})
}
func WriteJSON(path string, value any) error {
	return atomicWrite(path, func(w io.Writer) error {
		e := json.NewEncoder(w)
		e.SetIndent("", "  ")
		e.SetEscapeHTML(false)
		return e.Encode(value)
	})
}
func WriteText(path, s string) error {
	return atomicWrite(path, func(w io.Writer) error { _, err := io.WriteString(w, s); return err })
}
