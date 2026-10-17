package configstore

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// LoadDotenv reads a dotenv file (the seed format for a project's dev values).
func LoadDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m, err := ParseDotenv(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// ParseDotenv parses KEY=VALUE lines. It accepts blank lines, # comments, an
// optional `export ` prefix and single- or double-quoted values; anything after
// an unquoted " #" is a comment.
func ParseDotenv(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", n)
		}
		out[key] = unquote(strings.TrimSpace(val))
	}
	return out, sc.Err()
}

func unquote(v string) string {
	if len(v) >= 2 {
		switch {
		case v[0] == '"' && v[len(v)-1] == '"':
			return strings.ReplaceAll(v[1:len(v)-1], `\"`, `"`)
		case v[0] == '\'' && v[len(v)-1] == '\'':
			return v[1 : len(v)-1]
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}
