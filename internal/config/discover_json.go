package config

import (
	"bytes"
	"encoding/json"
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
	data, err = hujson.Standardize(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	if err != nil {
		return nil
	}
	return decodeObject(data)
}

func decodeObject(data []byte) map[string]any {
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	return doc
}
