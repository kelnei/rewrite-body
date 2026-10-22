package httputil

import "net/http"

// AddVary lists field in the Vary header, unless the header already covers it.
//
// It appends rather than replaces, so whatever the backend says about its own
// variation stays, and it checks first so a backend that already varies on field
// does not end up listing it twice. A Vary of "*" already says the response
// depends on everything, field included.
func AddVary(header http.Header, field string) {
	values := header.Values("Vary")

	if headerListsToken(values, "*") || headerListsToken(values, field) {
		return
	}

	header.Add("Vary", field)
}
