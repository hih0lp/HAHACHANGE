package main

import (
	"net/http"
	"net/url"
)

// Old form links re-enter the chat. The form opens inline after the browser
// acquires its demo session and team token in that same tab.
func formPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/?form="+url.QueryEscape(r.PathValue("id")), http.StatusSeeOther)
}
