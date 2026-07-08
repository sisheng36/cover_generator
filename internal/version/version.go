package version

import (
	"os"
	"path/filepath"
	"strings"
)

const defaultVersion = "Development version"

func Get() string {
	if value := strings.TrimSpace(os.Getenv("APP_VERSION")); value != "" {
		return value
	}

	for _, path := range versionCandidates() {
		if data, err := os.ReadFile(path); err == nil {
			if value := strings.TrimSpace(string(data)); value != "" {
				return value
			}
		}
	}

	return defaultVersion
}

func versionCandidates() []string {
	candidates := []string{
		filepath.Join("app", "VERSION"),
		"VERSION",
	}

	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for i := 0; i < 4; i++ {
			candidates = append(candidates, filepath.Join(dir, "app", "VERSION"))
			candidates = append(candidates, filepath.Join(dir, "VERSION"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 4; i++ {
			candidates = append(candidates, filepath.Join(dir, "app", "VERSION"))
			candidates = append(candidates, filepath.Join(dir, "VERSION"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}
