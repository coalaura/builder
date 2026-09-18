// Package metadata embeds key/value metadata in native executable structures.
package metadata

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Entry is one native metadata key/value pair.
type Entry struct {
	Key   string
	Value string
}

// Validate checks metadata keys and rejects duplicates.
func Validate(entries []Entry) error {
	seen := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		switch {
		case entry.Key == "":
			return fmt.Errorf("metadata key must not be empty")
		case len(entry.Key) > 16:
			return fmt.Errorf("metadata key %q exceeds 16 UTF-8 bytes", entry.Key)
		case strings.IndexByte(entry.Key, 0) >= 0:
			return fmt.Errorf("metadata key %q contains NUL", entry.Key)
		}

		if _, exists := seen[entry.Key]; exists {
			return fmt.Errorf("duplicate metadata key %q", entry.Key)
		}

		seen[entry.Key] = struct{}{}
	}

	return nil
}

// Embed detects the executable format and embeds entries using its native metadata mechanism.
func Embed(path string, entries []Entry) error {
	err := Validate(entries)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read binary for metadata: %w", err)
	}

	var embedded []byte

	switch {
	case len(data) >= 2 && data[0] == 'M' && data[1] == 'Z':
		embedded, err = embedPE(data, entries)
	case len(data) >= 4 && bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}):
		embedded, err = embedELF(data, entries)
	default:
		embedded, err = embedMachO(data, entries)
	}

	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat binary for metadata: %w", err)
	}

	err = os.WriteFile(path, embedded, info.Mode())
	if err != nil {
		return fmt.Errorf("write binary metadata: %w", err)
	}

	return nil
}
