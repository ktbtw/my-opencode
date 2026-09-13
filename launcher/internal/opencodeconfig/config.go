package opencodeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"launcher/internal/fileutil"
	"launcher/internal/paths"
)

const schemaURL = "https://opencode.ai/config.json"

func Dir() (string, error) {
	return paths.OpenCodeConfigDir()
}

func Path() (string, error) {
	root, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "opencode.json"), nil
}

func ReadText() (string, string, error) {
	path, err := migrate()
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return path, "{}\n", nil
	}
	if err != nil {
		return "", "", err
	}
	_ = os.Chmod(path, 0o600)
	return path, string(data), nil
}

func ReadMap() (string, string, map[string]any, error) {
	path, raw, err := ReadText()
	if err != nil {
		return "", "", nil, err
	}
	var parsed map[string]any
	if err := Decode([]byte(raw), &parsed); err != nil {
		return "", "", nil, err
	}
	if parsed == nil {
		parsed = map[string]any{}
	}
	return path, raw, parsed, nil
}

func RemoveProviderWhitelist() (bool, error) {
	_, _, parsed, err := ReadMap()
	if err != nil {
		return false, err
	}
	if _, ok := parsed["enabled_providers"]; !ok {
		return false, nil
	}
	delete(parsed, "enabled_providers")
	if _, _, err := WriteMap(parsed); err != nil {
		return false, err
	}
	return true, nil
}

func WriteMap(parsed map[string]any) (string, []byte, error) {
	path, err := Path()
	if err != nil {
		return "", nil, err
	}
	if parsed == nil {
		parsed = map[string]any{}
	}
	if strings.TrimSpace(asString(parsed["$schema"])) == "" {
		parsed["$schema"] = schemaURL
	}
	data, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return "", nil, err
	}
	if err := WriteRaw(path, append(data, '\n')); err != nil {
		return "", nil, err
	}
	for _, legacy := range legacyFiles(path) {
		_ = os.Remove(legacy)
	}
	return path, append(data, '\n'), nil
}

func WriteRaw(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

func Decode(data []byte, out any) error {
	cleaned, err := SanitizeJSONC(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(cleaned, out)
}

func SanitizeJSONC(data []byte) ([]byte, error) {
	withoutComments, err := stripJSONComments(data)
	if err != nil {
		return nil, err
	}
	return stripTrailingCommas(withoutComments), nil
}

func migrate() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	files := append([]string{path}, legacyFiles(path)...)
	changed := false
	merged := map[string]any{}
	for _, file := range filesInOrder(files) {
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		var parsed map[string]any
		if err := Decode(data, &parsed); err != nil {
			return "", err
		}
		if parsed == nil {
			continue
		}
		mergeMap(merged, parsed)
		if file != path {
			changed = true
		}
	}
	if !changed {
		return path, nil
	}
	if strings.TrimSpace(asString(merged["$schema"])) == "" {
		merged["$schema"] = schemaURL
	}
	if _, _, err := WriteMap(merged); err != nil {
		return "", err
	}
	return path, nil
}

func filesInOrder(files []string) []string {
	out := make([]string, 0, len(files))
	for _, file := range []string{files[1], files[0], files[2]} {
		if strings.TrimSpace(file) == "" {
			continue
		}
		out = append(out, file)
	}
	return out
}

func legacyFiles(path string) []string {
	dir := filepath.Dir(path)
	return []string{
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "opencode.jsonc"),
	}
}

func mergeMap(dst, src map[string]any) {
	for key, value := range src {
		next, ok := value.(map[string]any)
		if !ok {
			dst[key] = value
			continue
		}
		current, ok := dst[key].(map[string]any)
		if !ok {
			current = map[string]any{}
		}
		mergeMap(current, next)
		dst[key] = current
	}
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func stripJSONComments(data []byte) ([]byte, error) {
	var out strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(data) {
			next := data[i+1]
			if next == '/' {
				i += 2
				for ; i < len(data); i++ {
					if data[i] == '\n' {
						out.WriteByte('\n')
						break
					}
				}
				continue
			}
			if next == '*' {
				i += 2
				for ; i+1 < len(data); i++ {
					if data[i] == '*' && data[i+1] == '/' {
						i++
						break
					}
				}
				continue
			}
		}
		out.WriteByte(ch)
	}
	return []byte(out.String()), nil
}

func stripTrailingCommas(data []byte) []byte {
	var out strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for ; j < len(data); j++ {
				if data[j] == ' ' || data[j] == '\n' || data[j] == '\r' || data[j] == '\t' {
					continue
				}
				break
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
		}
		out.WriteByte(ch)
	}
	return []byte(out.String())
}
