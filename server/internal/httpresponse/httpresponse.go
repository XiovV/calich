// Package httpresponse provides the shared response shapes used across
// handlers, so error bodies stay consistent: {"error": {"code", "message"}}.
package httpresponse

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// File writes body as a download: contentType plus a Content-Disposition
// naming filename, so a browser/client gets a sensible default name even
// though it will typically build its own (ICS/zip export, #76).
func File(w http.ResponseWriter, status int, contentType, filename string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(status)
	w.Write(body)
}

// ICS writes body (already-encoded iCalendar bytes) as a text/calendar
// download named filename.
func ICS(w http.ResponseWriter, filename string, body []byte) {
	File(w, http.StatusOK, "text/calendar; charset=utf-8", filename, body)
}

// Zip writes body (an already-built zip archive) as an application/zip
// download named filename.
func Zip(w http.ResponseWriter, filename string, body []byte) {
	File(w, http.StatusOK, "application/zip", filename, body)
}

// Attachment streams body (the caller Closes it) as an Attachment download
// (#132, ADR-0040), preserving the stored contentType so a saved file keeps
// its association. On top of Content-Disposition, every response carries
// X-Content-Type-Options: nosniff and a sandboxed Content-Security-Policy —
// nothing user-uploaded ever renders inline from this origin, since an
// uploaded .html or .svg served inline would be stored XSS against the app
// itself.
func Attachment(w http.ResponseWriter, contentType, filename string, size int64, body io.Reader) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, body)
}
