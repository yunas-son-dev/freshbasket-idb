package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB
var tmpl *template.Template

type Incident struct {
	ID          int
	Title       string
	Severity    string
	Description string
	Status      string
	CreatedAt   string
}

type DashboardData struct {
	Incidents    []Incident
	Total        int
	Open         int
	InProgress   int
	Resolved     int
	SevLow       int
	SevMedium    int
	SevHigh      int
	SevCritical  int
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func initDB() {
	query := `CREATE TABLE IF NOT EXISTS incidents (
		id          INT AUTO_INCREMENT PRIMARY KEY,
		title       VARCHAR(255) NOT NULL,
		severity    ENUM('low','medium','high','critical') NOT NULL DEFAULT 'low',
		description TEXT,
		status      ENUM('open','in_progress','resolved') NOT NULL DEFAULT 'open',
		created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`
	if _, err := db.Exec(query); err != nil {
		log.Fatalf("Failed to initialise schema: %v", err)
	}
	log.Println("DB schema ready")
}

func main() {
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "3306")
	dbUser := getEnv("DB_USER", "admin")
	dbPassword := getEnv("DB_PASSWORD", "")
	dbName := getEnv("DB_NAME", "freshbasket")
	port := getEnv("PORT", "5000")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true",
		dbUser, dbPassword, dbHost, dbPort, dbName)

	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("DB open error: %v", err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatalf("DB ping error: %v", err)
	}
	log.Println("Connected to RDS MySQL")

	initDB()

	tmpl = template.Must(template.ParseGlob("templates/*.html"))

	mux := http.NewServeMux()
	mux.HandleFunc("/", listIncidents)
	mux.HandleFunc("/new", newIncidentForm)
	mux.HandleFunc("/incidents", createIncident)
	mux.HandleFunc("/incidents/", updateStatus) // /incidents/{id}/status
	mux.HandleFunc("/health", healthCheck)

	log.Printf("FreshBasket Incident Dashboard listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// GET / — list all incidents
func listIncidents(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	rows, err := db.Query(
		`SELECT id, title, severity, description, status,
		        DATE_FORMAT(created_at, '%Y-%m-%d %H:%i') AS created_at
		 FROM incidents ORDER BY created_at DESC`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var incidents []Incident
	for rows.Next() {
		var inc Incident
		if err := rows.Scan(&inc.ID, &inc.Title, &inc.Severity,
			&inc.Description, &inc.Status, &inc.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		incidents = append(incidents, inc)
	}

	data := DashboardData{Incidents: incidents, Total: len(incidents)}
	for _, inc := range incidents {
		switch inc.Status {
		case "open":
			data.Open++
		case "in_progress":
			data.InProgress++
		case "resolved":
			data.Resolved++
		}
		switch inc.Severity {
		case "low":
			data.SevLow++
		case "medium":
			data.SevMedium++
		case "high":
			data.SevHigh++
		case "critical":
			data.SevCritical++
		}
	}

	if err := tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// GET /new — show creation form
func newIncidentForm(w http.ResponseWriter, r *http.Request) {
	if err := tmpl.ExecuteTemplate(w, "new.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// POST /incidents — create incident
func createIncident(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	severity := r.FormValue("severity")
	description := strings.TrimSpace(r.FormValue("description"))
	status := r.FormValue("status")

	if title == "" {
		http.Error(w, "Title is required", http.StatusBadRequest)
		return
	}
	if status == "" {
		status = "open"
	}

	_, err := db.Exec(
		`INSERT INTO incidents (title, severity, description, status) VALUES (?, ?, ?, ?)`,
		title, severity, description, status,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// POST /incidents/{id}/status — update status
func updateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var id int
	// path: /incidents/42/status
	if _, err := fmt.Sscanf(r.URL.Path, "/incidents/%d/status", &id); err != nil || id == 0 {
		http.Error(w, "Invalid incident ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status := r.FormValue("status")

	if _, err := db.Exec(`UPDATE incidents SET status = ? WHERE id = ?`, status, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// GET /health — Elastic Beanstalk health check
func healthCheck(w http.ResponseWriter, r *http.Request) {
	if err := db.Ping(); err != nil {
		http.Error(w, "DB unreachable", http.StatusServiceUnavailable)
		return
	}
	fmt.Fprintln(w, "OK")
}
