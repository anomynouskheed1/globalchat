package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"
)

var templates = template.Must(
	template.ParseGlob(
		filepath.Join("templates", "*.html"),
	),
)

func Render(
	w http.ResponseWriter,
	r *http.Request,
	tmpl string,
	data interface{},
) {

	err := templates.ExecuteTemplate(w, tmpl, data)

	if err != nil {

		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)

		return
	}
}

func renderWithError(
	w http.ResponseWriter,
	r *http.Request,
	tmpl string,
	message string,
) {

	data := map[string]interface{}{
		"Error": message,
	}

	Render(w, r, tmpl, data)
}