package main

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

func detectMimeType(data []byte, path string) string {
	if len(data) > 0 {
		mt := http.DetectContentType(data)
		if mt != "application/octet-stream" {
			return mt
		}
	}

	if ext := strings.ToLower(filepath.Ext(path)); ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}

	return "image/jpeg"
}
