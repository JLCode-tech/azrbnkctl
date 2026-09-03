package k8s

import "bytes"

// SplitYAMLDocs splits a multi-document YAML byte stream on the "---" separator.
func SplitYAMLDocs(data []byte) [][]byte {
	parts := bytes.Split(data, []byte("\n---\n"))
	if len(parts) > 0 && bytes.HasPrefix(parts[0], []byte("---\n")) {
		parts[0] = parts[0][4:]
	}
	return parts
}
