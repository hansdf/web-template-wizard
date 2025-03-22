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
	templates := loadTemplates("data/default.json") // Load templates from default.json

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { // Handle the root route(main, and single page)
		if r.Method == http.MethodPost {
			r.ParseForm()
			selectedTemplates := r.Form["selected"]

			for i := range templates { // Update the IsSelected field for each template
				templates[i].IsSelected = false
				for _, selected := range selectedTemplates {
					if templates[i].Name == selected {
						templates[i].IsSelected = true
						break
					}
				}
			}
		}

		finalMessage := ""
		for _, template := range templates { // Generate the final message
			if template.IsSelected {
				finalMessage += template.Content + "\n\n"
			}
		}

		tmpl := template.Must(template.ParseFiles("index.html")) // Render the HTML template
		tmpl.Execute(w, PageData{
			Templates:    templates,
			FinalMessage: finalMessage,
		})
	})

	http.Handle("/styles.css", http.FileServer(http.Dir("."))) // Serve CSS file

	http.ListenAndServe(":8080", nil) // Start the server
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
