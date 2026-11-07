package httpx

import (
	"fmt"
	"net/http"

	"github.com/bete7512/hookbeam/pkg/apperr"
)

// Docs serves the Scalar API reference for the spec at specURL.
func Docs(specURL string) http.HandlerFunc {
	page := fmt.Sprintf(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>API reference</title></head>
<body>
  <script id="api-reference" data-url=%q></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`, specURL)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}
}

// NotFound answers unknown routes with a JSON 404.
func NotFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, apperr.NewNotFound("route", nil))
	}
}
