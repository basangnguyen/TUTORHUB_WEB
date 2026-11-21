package httpapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

type Problem struct {
	UnknownCredential bool   `json:"unknown_credential,omitempty"`
	Type              string `json:"type"`
	Code              string `json:"code,omitempty"`
	Title             string `json:"title"`
	Status            int    `json:"status"`
	Detail            string `json:"detail,omitempty"`
	Instance          string `json:"instance,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
}

func writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	title string,
	detail string,
) {
	writeCodedProblem(w, r, status, "", title, detail)
}

func writeCodedProblem(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code string,
	title string,
	detail string,
) {
	writeProblemWithSignal(w, r, status, code, title, detail, false)
}

func writeProblemWithSignal(w http.ResponseWriter, r *http.Request, status int, code, title, detail string, unknown bool) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	writeJSONBytes(w, status, Problem{
		UnknownCredential: unknown,
		Type:              problemType(status),
		Code:              code,
		Title:             title,
		Status:            status,
		Detail:            detail,
		Instance:          requestTelemetryPath(r.URL.Path),
		RequestID:         RequestIDFromContext(r.Context()),
	})
}

func writeJSON(logger *slog.Logger, w http.ResponseWriter, status int, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		// A custom Marshaler can include response contents in its error.
		logger.Error("marshal response", "response_type", fmt.Sprintf("%T", body))
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		writeJSONBytes(w, http.StatusInternalServerError, Problem{
			Type:   problemType(http.StatusInternalServerError),
			Title:  "Internal server error",
			Status: http.StatusInternalServerError,
			Detail: "The service could not encode the response.",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writePayload(w, status, payload)
}

func problemType(status int) string {
	return fmt.Sprintf("urn:tutorhub:problem:http-%d", status)
}

func writeJSONBytes(w http.ResponseWriter, status int, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writePayload(w, status, payload)
}

func writePayload(w http.ResponseWriter, status int, payload []byte) {
	w.WriteHeader(status)
	_, _ = w.Write(append(payload, '\n'))
}
