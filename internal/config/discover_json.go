package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"

	"github.com/tailscale/hujson"
)

// readJSON returns the object at name, or nil if the file is missing, unreadable or not a JSON object.
func readJSON(root fs.StatFS, name string) map[string]any {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return nil
	}
	return decodeObject(data)
}

// readJSONC is readJSON for files that may hold comments and trailing commas (deno.jsonc).
func readJSONC(root fs.StatFS, name string) map[string]any {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return nil
	}
	data, err = standardizeJSONC(data)
	if err != nil {
		return nil
	}
	return decodeObject(data)
}

// maxJSONDepth is the nesting limit of the JSON files package discovery parses with comments allowed.
const maxJSONDepth = 10000

// errJSONTooDeep is returned by standardizeJSONC for a document nested deeper than maxJSONDepth.
var errJSONTooDeep = errors.New("nesting is deeper than 10000 levels")

// standardizeJSONC turns JSON with comments and trailing commas into plain JSON. hujson recurses without a depth
// limit, so a file of a few MiB of `[` would overflow the stack, which no recover can catch: the nesting is counted
// first (see jsonDepthExceeded).
func standardizeJSONC(data []byte) ([]byte, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if jsonDepthExceeded(data, maxJSONDepth) {
		return nil, errJSONTooDeep
	}
	return hujson.Standardize(data)
}

// jsonDepthExceeded reports whether the brackets of data nest deeper than limit. Brackets inside strings, line
// comments and block comments do not count. It does not validate the document.
func jsonDepthExceeded(data []byte, limit int) bool {
	depth := 0
	for i := 0; i < len(data); i++ {
		switch c := data[i]; c {
		case '"':
			for i++; i < len(data) && data[i] != '"'; i++ {
				if data[i] == '\\' {
					i++
				}
			}
		case '/':
			switch {
			case i+1 < len(data) && data[i+1] == '/':
				if n := bytes.IndexByte(data[i:], '\n'); n < 0 {
					i = len(data)
				} else {
					i += n
				}
			case i+1 < len(data) && data[i+1] == '*':
				i += 2
				for i+1 < len(data) && (data[i] != '*' || data[i+1] != '/') {
					i++
				}
				i++
			}
		case '[', '{':
			if depth++; depth > limit {
				return true
			}
		case ']', '}':
			depth = max(depth-1, 0)
		}
	}
	return false
}

func decodeObject(data []byte) map[string]any {
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	return doc
}
