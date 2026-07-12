package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

//go:embed templates/*
var templateFiles embed.FS

func startServer(listenAddr, username, expectedPassHash string) {
	loginHTML, _ := templateFiles.ReadFile("templates/login.html")
	loginErrorHTML, _ := templateFiles.ReadFile("templates/login_error.html")
	dashboardHTML, _ := templateFiles.ReadFile("templates/dashboard.html")

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if !authenticated(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(dashboardHTML)
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(loginHTML)
			return
		}

		ip := getIP(r)
		if !rateLimit(ip) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			errorHTML := strings.Replace(string(loginErrorHTML), "Invalid username or password", "Too many attempts. Try again in 5 minutes.", 1)
			fmt.Fprint(w, errorHTML)
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
		w.Write(loginErrorHTML)
		log.Printf("Failed login attempt from IP %s", ip)
	})

	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		if !authenticated(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getMetricsHistory())
	})

	log.Printf("VPS Monitor starting on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatal(err)
	}
}
