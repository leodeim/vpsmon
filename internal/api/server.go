package api

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"vpsmon/internal/metrics"
	"vpsmon/misc"
)

//go:embed templates/*
var templateFiles embed.FS

// Register a skin here and add its matching stylesheet in templates/assets.
var supportedSkins = map[string]struct{}{
	"terminal": {},
	"modern":   {},
}

func StartServer(listenAddr, username, expectedPassHash, skin string, noAuth bool) {
	loginHTML, _ := templateFiles.ReadFile("templates/login.html")
	dashboardHTML, _ := templateFiles.ReadFile("templates/dashboard.html")
	if _, ok := supportedSkins[skin]; !ok {
		skin = "terminal"
	}
	dashboardHTML = []byte(strings.ReplaceAll(string(dashboardHTML), "{{SKIN}}", skin))
	logoutControl := `<a class="logout-link" href="/logout">Logout</a>`
	if noAuth {
		logoutControl = ""
	}
	dashboardHTML = []byte(strings.Replace(string(dashboardHTML), "{{LOGOUT_CONTROL}}", logoutControl, 1))
	loginHTML = []byte(strings.ReplaceAll(string(loginHTML), "{{SKIN}}", skin))

	mux := http.NewServeMux()
	isAuthorized := func(r *http.Request) bool {
		return noAuth || authenticated(r)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if !isAuthorized(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(dashboardHTML)
	})

	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		icon, err := misc.FS.ReadFile("icon.ico")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=2592000") // Cache for 30 days
		w.Write(icon)
	})

	// Stylesheets are public assets so the login page can be styled before authentication.
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(r.URL.Path, "/assets/")
		if name != "base.css" {
			if !strings.HasSuffix(name, ".css") {
				http.NotFound(w, r)
				return
			}
			if _, ok := supportedSkins[strings.TrimSuffix(name, ".css")]; !ok {
				http.NotFound(w, r)
				return
			}
		}
		if strings.Contains(name, "/") {
			http.NotFound(w, r)
			return
		}

		asset, err := templateFiles.ReadFile("templates/assets/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodGet {
			_, _ = w.Write(asset)
		}
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if noAuth {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(renderLoginPage(loginHTML, ""))
			return
		}

		ip := getIP(r)
		if !rateLimit(ip) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write(renderLoginPage(loginHTML, "Too many attempts. Try again in 5 minutes."))
			log.Printf("Blocked IP %s (rate limit)", ip)
			return
		}

		r.ParseForm()
		u := r.FormValue("username")
		p := r.FormValue("password")

		if subtle.ConstantTimeCompare([]byte(u), []byte(username)) == 1 {
			if err := bcrypt.CompareHashAndPassword([]byte(expectedPassHash), []byte(p)); err == nil {
				token := sessions.create()
				http.SetCookie(w, &http.Cookie{
					Name:     "session",
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
					MaxAge:   86400,
				})
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(renderLoginPage(loginHTML, "Invalid username or password"))
		log.Printf("Failed login attempt from IP %s", ip)
	})

	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		if noAuth {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if c, err := r.Cookie("session"); err == nil {
			sessions.destroy(c.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:   "session",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
		http.Redirect(w, r, "/login", http.StatusFound)
	})

	mux.HandleFunc("/api/metrics/stream", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		sendMetrics := func() {
			data, _ := json.Marshal(metrics.GetHistory())
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		}

		sendMetrics() // Send initial data immediately

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return // Client disconnected
			case <-ticker.C:
				if !isAuthorized(r) {
					return // Session expired, drop connection
				}
				sendMetrics()
			}
		}
	})

	mux.HandleFunc("/api/containers/", func(w http.ResponseWriter, r *http.Request) {
		if !isAuthorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/containers/"), "/logs")
		if r.Method != http.MethodGet || id == "" || r.URL.Path != "/api/containers/"+id+"/logs" {
			http.NotFound(w, r)
			return
		}

		logs, err := metrics.GetContainerLogs(id)
		if err != nil {
			http.Error(w, "unable to read container logs", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, logs)
	})

	if noAuth {
		log.Printf("WARNING: authentication is disabled")
	}
	log.Printf("vpsmon starting on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatal(err)
	}
}

func renderLoginPage(loginHTML []byte, errorMessage string) []byte {
	errorMarkup := ""
	if errorMessage != "" {
		errorMarkup = `<div class="login-error" role="alert">` + html.EscapeString(errorMessage) + `</div>`
	}
	return []byte(strings.Replace(string(loginHTML), "{{LOGIN_ERROR}}", errorMarkup, 1))
}
