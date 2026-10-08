package httpx

import "net/http"

// Fallback serves mux, but answers in the error shape when no route matches:
// 404 not_found for an unknown path, 405 method_not_allowed (with Allow) for a known
// path called with the wrong method. ServeMux itself would reply in plain text.
func Fallback(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		// No route. Let the mux decide between 404 and 405, but keep its plain-text body.
		probe := &probeWriter{header: http.Header{}}
		mux.ServeHTTP(probe, r)
		if probe.status == http.StatusMethodNotAllowed {
			w.Header().Set("Allow", probe.header.Get("Allow"))
			WriteError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "This method is not allowed for this resource.")
			return
		}
		WriteError(w, http.StatusNotFound, CodeNotFound, "The requested resource does not exist.")
	})
}

// probeWriter records the status and headers of a reply and discards its body.
type probeWriter struct {
	header http.Header
	status int
}

func (p *probeWriter) Header() http.Header { return p.header }

func (p *probeWriter) Write(b []byte) (int, error) { return len(b), nil }

func (p *probeWriter) WriteHeader(status int) {
	if p.status == 0 {
		p.status = status
	}
}
