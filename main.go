package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"os"
)

type Template struct {
	Name       string `json:"Name"`
	Content    string `json:"Content"`
	IsSelected bool   `json:"IsSelected"`
}

type PageData struct {
	Templates    []Template
	FinalMessage string
}

func main() {
	// Load templates from default.json
	templates := loadTemplates("data/default.json")

	// Handle the root route(main, and single page)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			// Parse the form data
			r.ParseForm()
			selectedTemplates := r.Form["selected"]

			// Update the IsSelected field for each template
			for i := range templates {
				templates[i].IsSelected = false
				for _, selected := range selectedTemplates {
					if templates[i].Name == selected {
						templates[i].IsSelected = true
						break
					}
				}
			}
		}

		// Generate the final message
		finalMessage := ""
		for _, template := range templates {
			if template.IsSelected {
				finalMessage += template.Content + "\n\n"
			}
		}

		// Render the HTML template
		tmpl := template.Must(template.ParseFiles("index.html"))
		tmpl.Execute(w, PageData{
			Templates:    templates,
			FinalMessage: finalMessage,
		})
	})

	// Serve CSS file
	http.Handle("/styles.css", http.FileServer(http.Dir(".")))

	// Start the server
	http.ListenAndServe(":8080", nil)
}

func loadTemplates(filename string) []Template {
	file, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	var templates []Template
	err = json.Unmarshal(file, &templates)
	if err != nil {
		panic(err)
	}

	return templates
}
