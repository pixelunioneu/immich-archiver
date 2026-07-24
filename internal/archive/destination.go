package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sidecarName returns the sidecar filename for a given asset filename, e.g.
// "IMG_0001.jpg" -> "IMG_0001.jpg.json".
func sidecarName(filename string) string {
	return filename + ".json"
}

// sidecarAssetID peeks at an existing sidecar file's "id" field, returning
// "" if the file doesn't exist or can't be parsed.
func sidecarAssetID(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return ""
	}
	return v.ID
}

// ResolveDestination decides the on-disk filename for assetID/originalName
// within dir. "Already downloaded" is determined purely from the
// filesystem: if a file with the candidate name exists and its sidecar's
// recorded asset id matches assetID, the asset is considered already
// present. If the name is taken by a *different* asset, a numeric suffix
// (_1, _2, ...) is appended to originalName until a free or matching name is
// found.
func ResolveDestination(dir, originalName, assetID string) (filename string, alreadyExists bool, err error) {
	ext := filepath.Ext(originalName)
	base := strings.TrimSuffix(originalName, ext)

	for n := 0; ; n++ {
		candidate := originalName
		if n > 0 {
			candidate = fmt.Sprintf("%s_%d%s", base, n, ext)
		}

		fullPath := filepath.Join(dir, candidate)
		if _, statErr := os.Stat(fullPath); statErr != nil {
			if os.IsNotExist(statErr) {
				return candidate, false, nil
			}
			return "", false, fmt.Errorf("checking %s: %w", fullPath, statErr)
		}

		if sidecarAssetID(filepath.Join(dir, sidecarName(candidate))) == assetID {
			return candidate, true, nil
		}
		// Name taken by a different asset; try the next suffix.
	}
}
