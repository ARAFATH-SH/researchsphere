package httpx

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// Stream sends a long response body that is written as it is made: a ZIP archive, a PDF, a
// tar for the compile worker. It lifts the server's write timeout (15 s) to deadline, sets
// header's fields and status 200, then runs body. Once the status is sent an error can no
// longer become an error response: Stream aborts the connection instead (the client sees a
// cut response, never a broken file that looks whole), and net/http logs the error.
func Stream(w http.ResponseWriter, deadline time.Duration, header http.Header, body func(io.Writer) error) error {
	// A writer that cannot set a deadline (a test recorder) keeps the server's.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(deadline))
	for k, v := range header {
		w.Header()[k] = v
	}
	w.WriteHeader(http.StatusOK)
	if err := body(w); err != nil {
		panic(fmt.Errorf("%w: stream: %w", http.ErrAbortHandler, err))
	}
	return nil
}
