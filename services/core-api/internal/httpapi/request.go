package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

func decodeJSONRequest(
	w http.ResponseWriter,
	r *http.Request,
	destination any,
	maximumBytes int64,
) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maximumBytes))
	var payload json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	if err := validateJSONObject(payload); err != nil {
		return err
	}

	strict := json.NewDecoder(bytes.NewReader(payload))
	strict.DisallowUnknownFields()
	if err := strict.Decode(destination); err != nil {
		return err
	}
	if err := strict.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}

	return nil
}

// validateJSONObject rejects scalar/array/null payloads and duplicate fields before
// the endpoint-specific strict decoder runs. Duplicate rejection avoids parser
// ambiguity at authorization and mutation boundaries (for example two tenant_id or
// expected_version fields with different values).
func validateJSONObject(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := validateJSONValue(decoder, true); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder, requireObject bool) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if requireObject && (!isDelimiter || delimiter != '{') {
		return errors.New("request must contain one JSON object")
	}
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		fields := make(map[string]struct{})
		for decoder.More() {
			fieldToken, err := decoder.Token()
			if err != nil {
				return err
			}
			field, ok := fieldToken.(string)
			if !ok {
				return errors.New("request contains an invalid JSON object field")
			}
			// encoding/json resolves object keys against struct fields without
			// case sensitivity. Apply the same folding here so payloads such as
			// {"field":"one","FIELD":"two"} cannot rely on
			// last-value-wins behavior at a mutation boundary.
			fieldKey := strings.ToLower(field)
			if _, duplicate := fields[fieldKey]; duplicate {
				return fmt.Errorf("request contains duplicate JSON field %q", field)
			}
			fields[fieldKey] = struct{}{}
			if err := validateJSONValue(decoder, false); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("request contains an invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder, false); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("request contains an invalid JSON array")
		}
	default:
		return errors.New("request contains an invalid JSON delimiter")
	}

	return nil
}
