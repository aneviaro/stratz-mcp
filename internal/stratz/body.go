package stratz

import (
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
)

type countingReader struct {
	reader io.Reader
	count  int64
	limit  int64
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	if reader.count >= reader.limit {
		return 0, errWireLimit
	}
	remaining := reader.limit - reader.count
	if int64(len(buffer)) > remaining+1 {
		buffer = buffer[:remaining+1]
	}
	count, err := reader.reader.Read(buffer)
	reader.count += int64(count)
	if reader.count > reader.limit {
		return count, errWireLimit
	}
	return count, err
}

var errWireLimit = errors.New("wire response limit exceeded")

func readBoundedBody(response *http.Response, wireLimit, decodedLimit int64) ([]byte, *Error) {
	wire := &countingReader{
		reader: response.Body,
		limit:  wireLimit,
	}
	var decoded io.Reader = wire

	encoding := strings.TrimSpace(strings.ToLower(headerValue(response.Header, "Content-Encoding")))
	switch encoding {
	case "", "identity":
	case "gzip":
		gzipReader, err := gzip.NewReader(bufio.NewReader(wire))
		if err != nil {
			return nil, protocolError("STRATZ returned an invalid gzip response", err)
		}
		defer gzipReader.Close()
		decoded = gzipReader
	default:
		return nil, protocolError("STRATZ returned an unsupported content encoding", nil)
	}

	body, err := io.ReadAll(io.LimitReader(decoded, decodedLimit+1))
	switch {
	case errors.Is(err, errWireLimit):
		return body, responseTooLargeError("The upstream wire response exceeded its safety limit")
	case err != nil:
		return body, protocolError("Could not read the upstream response body", err)
	case int64(len(body)) > decodedLimit:
		return body, responseTooLargeError("The decompressed upstream response exceeded its safety limit")
	default:
		return body, nil
	}
}

func responseTooLargeError(message string) *Error {
	return &Error{
		Code:      contracts.ErrorCodeResponseTooLarge,
		Message:   message,
		Details:   map[string]any{},
		Retryable: false,
	}
}

func isJSONMediaType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" ||
		mediaType == "application/graphql-response+json" ||
		strings.HasSuffix(mediaType, "+json")
}
