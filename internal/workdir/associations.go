package workdir

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/marcus/td/internal/syncconfig"
)

const associationsFile = "associations.json"

// LoadAssociations reads the directory associations from
// ~/.config/td/associations.json. Returns an empty map if the file
// does not exist.
func LoadAssociations() (map[string]string, error) {
	dir, err := syncconfig.ConfigDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, associationsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	var assoc map[string]string
	if err := json.Unmarshal(data, &assoc); err != nil {
		return nil, err
	}
	return assoc, nil
}

// SaveAssociations writes the directory associations to
// ~/.config/td/associations.json using atomic write.
func SaveAssociations(assoc map[string]string) error {
	dir, err := syncconfig.ConfigDir()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(assoc, "", "  ")
	if err != nil {
		return err
	}

	target := filepath.Join(dir, associationsFile)

	// Atomic write: temp file + rename
	tmp, err := os.CreateTemp(dir, "associations-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	return os.Rename(tmpName, target)
}

// LookupAssociation checks if dir has a directory association configured.
// Returns the target path and true if found, empty string and false otherwise.
func LookupAssociation(dir string) (string, bool) {
	assoc, err := LoadAssociations()
	if err != nil {
		return "", false
	}

	// Normalize the lookup key
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}

	target, ok := assoc[dir]
	if !ok {
		return "", false
	}

	// Normalize the target
	if !filepath.IsAbs(target) {
		return "", false
	}
	return filepath.Clean(target), true
}
