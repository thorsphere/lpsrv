// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv

// Import packages
import (
	"encoding/json" // Import the encoding/json package for JSON decoding and buffered encoding.
	"errors"        // Import the errors package for error matching and construction.
	"io"            // Import the io package for EOF checks on trailing JSON tokens.
	"net/http"      // Import the net/http package for body size limits and HTTP status codes.

	"github.com/thorsphere/tserr" // Import the tserr package for structured error construction.
	"github.com/thorsphere/tslog" // Import the tslog package for structured logging.
)

// jsonContentType is the HTTP Content-Type header value written on JSON responses.
const jsonContentType = "application/json"

// DecodeJSON returns the single JSON value of type T decoded from the body of
// r, enforcing a positive maxBytes limit via w and rejecting unknown fields
// and trailing data. On success it returns a zero status code and nil error.
// On failure it returns the zero value of T and a structured error, with status
// 400 for invalid request bodies, 413 for oversized bodies, or 500 if maxBytes
// is zero or negative.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request, maxBytes int64) (T, int, error) {
	// Prepare the zero value of T for error returns.
	var zero T
	// Validate the caller-supplied limit before accessing the request body.
	if maxBytes <= 0 {
		// Report the nonpositive limit as a server configuration error.
		return zero, http.StatusInternalServerError, tserr.Higher(&tserr.HigherArgs{
			Var:        "maxBytes",
			Actual:     maxBytes,
			LowerBound: 1,
		})
	}
	// Reject nil requests or requests carrying a nil body stream.
	if r == nil || r.Body == nil {
		// Return 400 Bad Request when no request body is available.
		return zero, http.StatusBadRequest, tserr.Empty("request body")
	}
	// Bound the request body size so oversized payloads are rejected without buffering.
	body := http.MaxBytesReader(w, r.Body, maxBytes)
	// Build a streaming JSON decoder over the size-bounded body.
	dec := json.NewDecoder(body)
	// Reject unknown JSON object fields so misspelled keys fail fast.
	dec.DisallowUnknownFields()
	// Prepare the destination variable for the decoded JSON payload.
	var payload T
	// Decode the primary JSON value from the bounded request body.
	if err := dec.Decode(&payload); err != nil {
		// Prepare to match an oversized-body error from MaxBytesReader.
		var maxErr *http.MaxBytesError
		// Check whether reading the payload exceeded maxBytes.
		if errors.As(err, &maxErr) {
			// Reject oversized payloads with 413 Request Entity Too Large.
			return zero, http.StatusRequestEntityTooLarge, tserr.TooLarge(&tserr.TooLargeArgs{
				F:        "request body",
				Actual:   maxErr.Limit + 1,
				MaxBytes: maxErr.Limit,
			})
		}
		// Reject malformed or empty JSON bodies with 400 Bad Request.
		return zero, http.StatusBadRequest, tserr.InvalidJson(err)
	}
	// Check that no trailing data follows the single JSON value.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		// Prepare to match an oversized-body error encountered while scanning trailing bytes.
		var maxErr *http.MaxBytesError
		// Check whether trailing whitespace or data pushed the body past maxBytes.
		if errors.As(err, &maxErr) {
			// Reject oversized payloads with 413 Request Entity Too Large.
			return zero, http.StatusRequestEntityTooLarge, tserr.TooLarge(&tserr.TooLargeArgs{
				F:        "request body",
				Actual:   maxErr.Limit + 1,
				MaxBytes: maxErr.Limit,
			})
		}
		// Reject trailing JSON tokens with 400 Bad Request.
		return zero, http.StatusBadRequest, tserr.InvalidJson(errors.New("unexpected trailing data"))
	}
	// Return the validated payload, zero status, and nil error to indicate success.
	return payload, 0, nil
}

// WriteJSON marshals payload to JSON in memory before writing headers, responds
// with status (defaulting to 200 OK when status is zero or negative) and an
// application/json Content-Type on w, omits the body when r is a HEAD request,
// and logs and writes a sanitized 500 Internal Server Error if marshaling fails.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	// Marshal payload in memory before sending headers so encoding failures can return 500.
	jsonBytes, err := json.Marshal(payload)
	// If JSON encoding fails, log the error and respond with a sanitized 500 status.
	if err != nil {
		// Write a sanitized 500 Internal Server Error before any 2xx headers are sent.
		WriteInternalError(w, tserr.Op(&tserr.OpArgs{Op: "json.Marshal", Fn: requestPath(r), Err: err}))
		// Stop processing after writing the error response.
		return
	}
	// Default non-positive status codes to 200 OK.
	if status <= 0 {
		// Use 200 OK when no explicit status code was supplied.
		status = http.StatusOK
	}
	// Set the JSON Content-Type header before writing the status line.
	w.Header().Set(contentTypeHeader, jsonContentType)
	// Write the HTTP status code header.
	w.WriteHeader(status)
	// Omit the response body on HEAD requests per RFC 9110 semantics.
	if r != nil && r.Method == http.MethodHead {
		// Return without writing body bytes on HEAD requests.
		return
	}
	// Write the buffered JSON response body and log any write failure.
	if _, err := w.Write(jsonBytes); err != nil {
		// Log the response write error for diagnostics after headers have been sent.
		tslog.Error(tserr.Op(&tserr.OpArgs{Op: "write response", Fn: requestPath(r), Err: err}))
	}
}
