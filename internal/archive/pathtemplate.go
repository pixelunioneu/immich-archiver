package archive

import (
	"fmt"
	"strings"
	"time"
)

// DefaultPathTemplate is applied when the user doesn't override it via flag.
const DefaultPathTemplate = "{year}/{year}-{month}"

var pathTokens = map[string]string{
	"{year}":  "2006",
	"{month}": "01",
	"{day}":   "02",
}

// RenderPath expands a friendly token template (e.g. "{year}/{year}-{month}")
// against t, returning a slash-separated relative directory path.
func RenderPath(tmpl string, t time.Time) (string, error) {
	if tmpl == "" {
		return "", fmt.Errorf("path template must not be empty")
	}
	out := tmpl
	for token, layout := range pathTokens {
		out = strings.ReplaceAll(out, token, t.Format(layout))
	}
	if strings.Contains(out, "{") || strings.Contains(out, "}") {
		return "", fmt.Errorf("path template %q contains unknown token(s); supported tokens: {year}, {month}, {day}", tmpl)
	}
	return out, nil
}
