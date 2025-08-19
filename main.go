package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type User struct {
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	IsAdmin      bool
}

type TemplateRow struct {
	ID        int64
	Username  string
	Name      string
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UITemplate struct {
	Name       string `json:"Name"`
	Content    string `json:"Content"`
	IsSelected bool   `json:"IsSelected"`
}

type PageData struct {
	Templates    []UITemplate
	FinalMessage string
	Username     string
}

var (
	db              *sql.DB
	sessionManager  *SessionManager
	logger          = log.New(os.Stdout, "", log.LstdFlags)
	defaultJSONPath = "data/default.json"
)

func main() {
	// CLI flags for admin user creation
	createUserFlag := flag.Bool("create-user", false, "Create a user and exit")
	usernameFlag := flag.String("username", "", "Username for the user to create")
	passwordFlag := flag.String("password", "", "Password for the user to create")
	adminFlag := flag.Bool("admin", false, "Mark created user as admin (with -create-user)")
	makeAdminFlag := flag.Bool("make-admin", false, "Promote an existing user to admin and exit")
	usernameForAdminFlag := flag.String("username-admin", "", "Username for admin operations (with -make-admin)")
	lookupUserFlag := flag.Bool("lookup-user", false, "Lookup user by -username and exit")
	flag.Parse()

	// Ensure data directory exists
	_ = os.MkdirAll("data", 0o755)

	// Init DB
	var err error
	db, err = sql.Open("sqlite", filepath.ToSlash(filepath.Clean("data/app.db")))
	if err != nil {
		logger.Fatalf("open db: %v", err)
	}
	if err := migrate(db); err != nil {
		logger.Fatalf("migrate: %w", err)
	}

	if *createUserFlag {
		if strings.TrimSpace(*usernameFlag) == "" || strings.TrimSpace(*passwordFlag) == "" {
			logger.Fatalf("-username and -password are required with -create-user")
		}
		if err := createUserWithAdmin(db, *usernameFlag, *passwordFlag, *adminFlag); err != nil {
			logger.Fatalf("create user: %v", err)
		}
		logger.Printf("Created user %s (admin=%v)", *usernameFlag, *adminFlag)
		return
	}
	if *lookupUserFlag {
		if strings.TrimSpace(*usernameFlag) == "" {
			logger.Fatalf("-username is required with -lookup-user")
		}
		u, err := getUserByUsername(db, *usernameFlag)
		if err != nil {
			logger.Fatalf("lookup failed: %v", err)
		}
		fmt.Printf("User found: %s (admin=%v)\n", u.Username, u.IsAdmin)
		return
	}
	if *makeAdminFlag {
		if *usernameForAdminFlag == "" {
			logger.Fatalf("-username-admin is required with -make-admin")
		}
		if err := setUserAdmin(db, *usernameForAdminFlag, true); err != nil {
			logger.Fatalf("make admin: %v", err)
		}
		logger.Printf("User %s promoted to admin", *usernameForAdminFlag)
		return
	}

	// Sessions
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		sessionKey = "dev-secret-change-me"
	}
	sessionManager = NewSessionManager([]byte(sessionKey))

	// Routes
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/logout", handleLogout)

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Test route for button layout
	http.HandleFunc("/test-buttons", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "test_buttons.html")
	})

	// Templates CRUD
	http.HandleFunc("/templates", withAuth(handleTemplatesList))
	http.HandleFunc("/templates/new", withAuth(handleTemplateCreate))
	http.HandleFunc("/templates/edit", withAuth(handleTemplateEdit))     // expects ?id=123
	http.HandleFunc("/templates/delete", withAuth(handleTemplateDelete)) // POST expects id

	// Admin: user management
	http.HandleFunc("/admin/users", withAdmin(handleAdminUsersList))
	http.HandleFunc("/admin/users/new", withAdmin(handleAdminUsersNew))
	http.HandleFunc("/admin/users/reset-password", withAdmin(handleAdminUsersResetPassword))
	http.HandleFunc("/admin/users/delete", withAdmin(handleAdminUsersDelete))
	http.HandleFunc("/admin/users/toggle-admin", withAdmin(handleAdminUsersToggleAdmin))

	// Root page: selection and output
	http.HandleFunc("/", withAuth(func(w http.ResponseWriter, r *http.Request, user User) {
		templates, err := listTemplatesByUser(r.Context(), db, user.Username)
		if err != nil {
			http.Error(w, "failed to load templates", http.StatusInternalServerError)
			return
		}

		// If user has no templates, seed from default.json
		if len(templates) == 0 {
			if err := seedUserTemplatesFromJSON(r.Context(), db, user.Username, defaultJSONPath); err != nil {
				http.Error(w, "failed to seed templates", http.StatusInternalServerError)
				return
			}
			templates, _ = listTemplatesByUser(r.Context(), db, user.Username)
		}

		uiTemplates := make([]UITemplate, 0, len(templates))
		for _, t := range templates {
			uiTemplates = append(uiTemplates, UITemplate{Name: t.Name, Content: t.Content})
		}

		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			selected := map[string]struct{}{}
			for _, s := range r.Form["selected"] {
				selected[s] = struct{}{}
			}
			for i := range uiTemplates {
				if _, ok := selected[uiTemplates[i].Name]; ok {
					uiTemplates[i].IsSelected = true
				}
			}
		}

		var finalMessage strings.Builder
		for _, t := range uiTemplates {
			if t.IsSelected {
				finalMessage.WriteString(t.Content)
				finalMessage.WriteString("\n\n")
			}
		}

		tmpl := template.Must(template.ParseFiles("index.html"))
		_ = tmpl.Execute(w, PageData{Templates: uiTemplates, FinalMessage: finalMessage.String(), Username: user.Username})
	}))

	addr := ":8080"
	logger.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Fatal(err)
	}
}

// ===== DB and Models =====

func migrate(db *sql.DB) error {
	// Create tables if they don't exist
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
            username TEXT PRIMARY KEY,
            password_hash TEXT NOT NULL,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            is_admin INTEGER NOT NULL DEFAULT 0
        );`,
		`CREATE TABLE IF NOT EXISTS templates (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            username TEXT NOT NULL,
            name TEXT NOT NULL,
            content TEXT NOT NULL,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
        );`,
		`CREATE INDEX IF NOT EXISTS idx_templates_username ON templates(username);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("create tables: %w", err)
		}
	}
	return nil
}

func getUserByUsername(db *sql.DB, username string) (*User, error) {
	row := db.QueryRow("SELECT username, password_hash, created_at, COALESCE(is_admin,0) FROM users WHERE username = ?", username)
	var u User
	if err := row.Scan(&u.Username, &u.PasswordHash, &u.CreatedAt, &u.IsAdmin); err != nil {
		return nil, err
	}
	return &u, nil
}

func createUser(db *sql.DB, username, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT INTO users(username, password_hash, is_admin) VALUES(?, ?, 0)", username, hash)
	return err
}

func createUserWithAdmin(db *sql.DB, username, password string, isAdmin bool) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	adminVal := 0
	if isAdmin {
		adminVal = 1
	}
	_, err = db.Exec("INSERT INTO users(username, password_hash, is_admin) VALUES(?, ?, ?)", username, hash, adminVal)
	return err
}

func listTemplatesByUser(ctx context.Context, db *sql.DB, username string) ([]TemplateRow, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, username, name, content, created_at, updated_at FROM templates WHERE username = ? ORDER BY id", username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TemplateRow
	for rows.Next() {
		var t TemplateRow
		if err := rows.Scan(&t.ID, &t.Username, &t.Name, &t.Content, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func getTemplateByID(ctx context.Context, db *sql.DB, username string, id int64) (*TemplateRow, error) {
	row := db.QueryRowContext(ctx, "SELECT id, username, name, content, created_at, updated_at FROM templates WHERE username = ? AND id = ?", username, id)
	var t TemplateRow
	if err := row.Scan(&t.ID, &t.Username, &t.Name, &t.Content, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func createTemplate(ctx context.Context, db *sql.DB, username, name, content string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name required")
	}
	_, err := db.ExecContext(ctx, "INSERT INTO templates(username, name, content) VALUES(?, ?, ?)", username, name, content)
	return err
}

func updateTemplate(ctx context.Context, db *sql.DB, username string, id int64, name, content string) error {
	_, err := db.ExecContext(ctx, "UPDATE templates SET name = ?, content = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND username = ?", name, content, id, username)
	return err
}

func deleteTemplate(ctx context.Context, db *sql.DB, username string, id int64) error {
	_, err := db.ExecContext(ctx, "DELETE FROM templates WHERE id = ? AND username = ?", id, username)
	return err
}

func seedUserTemplatesFromJSON(ctx context.Context, db *sql.DB, username string, jsonPath string) error {
	bytes, err := os.ReadFile(jsonPath)
	if err != nil {
		return err
	}
	var items []UITemplate
	if err := json.Unmarshal(bytes, &items); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, it := range items {
		if strings.TrimSpace(it.Name) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO templates(username, name, content) VALUES(?, ?, ?)", username, it.Name, it.Content); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ===== Auth & Sessions =====

func withAuth(next func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, err := sessionManager.GetUsernameFromRequest(r)
		if err != nil || username == "" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		u, err := getUserByUsername(db, username)
		if err != nil {
			sessionManager.Clear(w)
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r, *u)
	}
}

func withAdmin(next func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return withAuth(func(w http.ResponseWriter, r *http.Request, u User) {
		if !u.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r, u)
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		renderTemplate(w, "templates/login.html", map[string]any{"Error": ""})
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		// Authenticate by username instead of numeric user ID
		username := strings.TrimSpace(r.Form.Get("username"))
		password := r.Form.Get("password")

		if username == "" {
			renderTemplate(w, "templates/login.html", map[string]any{"Error": "username required"})
			return
		}

		u, err := getUserByUsername(db, username)
		if err != nil {
			renderTemplate(w, "templates/login.html", map[string]any{"Error": "invalid credentials"})
			return
		}
		if err := comparePassword(u.PasswordHash, password); err != nil {
			renderTemplate(w, "templates/login.html", map[string]any{"Error": "invalid credentials"})
			return
		}
		// Seed templates if needed
		count := 0
		_ = db.QueryRow("SELECT COUNT(1) FROM templates WHERE username = ?", u.Username).Scan(&count)
		if count == 0 {
			_ = seedUserTemplatesFromJSON(r.Context(), db, u.Username, defaultJSONPath)
		}
		if err := sessionManager.SetUsername(w, u.Username); err != nil {
			http.Error(w, "failed to set session", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	sessionManager.Clear(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// Templates CRUD handlers
func handleTemplatesList(w http.ResponseWriter, r *http.Request, user User) {
	switch r.Method {
	case http.MethodGet:
		items, err := listTemplatesByUser(r.Context(), db, user.Username)
		if err != nil {
			http.Error(w, "failed to load", 500)
			return
		}
		renderTemplate(w, "templates/templates_list.html", map[string]any{"Templates": items, "Username": user.Username})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleTemplateCreate(w http.ResponseWriter, r *http.Request, user User) {
	switch r.Method {
	case http.MethodGet:
		renderTemplate(w, "templates/template_form.html", map[string]any{"Action": "Create", "Template": TemplateRow{}})
	case http.MethodPost:
		_ = r.ParseForm()
		name := strings.TrimSpace(r.Form.Get("name"))
		content := r.Form.Get("content")
		if err := createTemplate(r.Context(), db, user.Username, name, content); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "/templates", http.StatusFound)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleTemplateEdit(w http.ResponseWriter, r *http.Request, user User) {
	idStr := r.URL.Query().Get("id")
	var id int64
	_, _ = fmt.Sscan(idStr, &id)
	switch r.Method {
	case http.MethodGet:
		t, err := getTemplateByID(r.Context(), db, user.Username, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		renderTemplate(w, "templates/template_form.html", map[string]any{"Action": "Edit", "Template": t})
	case http.MethodPost:
		_ = r.ParseForm()
		name := strings.TrimSpace(r.Form.Get("name"))
		content := r.Form.Get("content")
		if err := updateTemplate(r.Context(), db, user.Username, id, name, content); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "/templates", http.StatusFound)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleTemplateDelete(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	var id int64
	_, _ = fmt.Sscan(r.Form.Get("id"), &id)
	if err := deleteTemplate(r.Context(), db, user.Username, id); err != nil {
		http.Error(w, "failed to delete", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/templates", http.StatusFound)
}

func renderTemplate(w http.ResponseWriter, file string, data any) {
	tmpl := template.Must(template.ParseFiles(file))
	_ = tmpl.Execute(w, data)
}

// ===== Admin: users management =====

func listAllUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, "SELECT username, password_hash, created_at, COALESCE(is_admin,0) FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Username, &u.PasswordHash, &u.CreatedAt, &u.IsAdmin); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func resetUserPassword(ctx context.Context, db *sql.DB, username string, newPassword string) error {
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE username = ?", hash, username)
	return err
}

func deleteUser(ctx context.Context, db *sql.DB, username string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM users WHERE username = ?", username)
	return err
}

func setUserAdmin(db *sql.DB, username string, isAdmin bool) error {
	val := 0
	if isAdmin {
		val = 1
	}
	_, err := db.Exec("UPDATE users SET is_admin = ? WHERE username = ?", val, username)
	return err
}

func handleAdminUsersList(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	users, err := listAllUsers(r.Context(), db)
	if err != nil {
		http.Error(w, "failed to load users", 500)
		return
	}
	renderTemplate(w, "templates/admin_users.html", map[string]any{"Users": users})
}

func handleAdminUsersNew(w http.ResponseWriter, r *http.Request, user User) {
	switch r.Method {
	case http.MethodGet:
		renderTemplate(w, "templates/admin_user_form.html", map[string]any{"Error": ""})
	case http.MethodPost:
		_ = r.ParseForm()
		email := strings.TrimSpace(r.Form.Get("email"))
		password := r.Form.Get("password")
		isAdmin := r.Form.Get("is_admin") == "on"
		if err := createUserWithAdmin(db, email, password, isAdmin); err != nil {
			renderTemplate(w, "templates/admin_user_form.html", map[string]any{"Error": err.Error()})
			return
		}
		http.Redirect(w, r, "/admin/users", http.StatusFound)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleAdminUsersResetPassword(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	var id int64
	_, _ = fmt.Sscan(r.Form.Get("id"), &id)
	newPassword := r.Form.Get("new_password")
	if err := resetUserPassword(r.Context(), db, user.Username, newPassword); err != nil {
		http.Error(w, "failed to reset password", 400)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusFound)
}

func handleAdminUsersDelete(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	var id int64
	_, _ = fmt.Sscan(r.Form.Get("id"), &id)
	if err := deleteUser(r.Context(), db, user.Username); err != nil {
		http.Error(w, "failed to delete user", 400)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusFound)
}

func handleAdminUsersToggleAdmin(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	var id int64
	_, _ = fmt.Sscan(r.Form.Get("id"), &id)
	u, err := getUserByUsername(db, r.Form.Get("username"))
	if err != nil {
		http.Error(w, "user not found", 404)
		return
	}
	if err := setUserAdmin(db, u.Username, !u.IsAdmin); err != nil {
		http.Error(w, "failed to update", 400)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusFound)
}
