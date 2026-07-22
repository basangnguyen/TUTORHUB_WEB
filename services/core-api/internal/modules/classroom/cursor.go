package classroom

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// decodeStrictCursorJSON keeps cursor payloads forward-compatible only through
// an explicit decoder change. Cursors are client-controlled input, so silently
// accepting unknown fields would make malformed or mixed-version cursors harder
// to reject consistently across classroom and roster pagination.
func decodeStrictCursorJSON(contents []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("cursor must contain one JSON value")
	}
	return nil
}
