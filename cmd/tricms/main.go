// Command tricms starts the triCMS server: system.db bootstrap, the JSON
// REST API, and the server-rendered HTMX admin UI, all in one binary.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	"tricms/pkg/api"
	"tricms/pkg/auth"
	"tricms/pkg/storage"
	"tricms/pkg/webhooks"
	"tricms/web"
)

func main() {
	dataDir := envOr("TRICMS_DATA_DIR", "./data")
	addr := envOr("TRICMS_ADDR", ":8080")
	jwtSecret := os.Getenv("TRICMS_JWT_SECRET")
	encryptionKey := os.Getenv("TRICMS_ENCRYPTION_KEY")

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		log.Fatalf("create data directory: %v", err)
	}

	if jwtSecret == "" {
		generated, err := randomSecret()
		if err != nil {
			log.Fatalf("generate session secret: %v", err)
		}
		jwtSecret = generated
		log.Println("WARNING: TRICMS_JWT_SECRET is not set; using an ephemeral secret.")
		log.Println("Sessions will be invalidated on restart. Set TRICMS_JWT_SECRET for production.")
	}

	if encryptionKey == "" {
		generated, err := randomSecret()
		if err != nil {
			log.Fatalf("generate encryption key: %v", err)
		}
		encryptionKey = generated
		log.Println("WARNING: TRICMS_ENCRYPTION_KEY is not set; using an ephemeral key.")
		log.Println("Secrets stored in github_dispatch webhooks (GitHub tokens) will become")
		log.Println("undecryptable on restart. Set TRICMS_ENCRYPTION_KEY for production.")
	}

	system, err := storage.OpenSystemDB(dataDir + "/system.db")
	if err != nil {
		log.Fatalf("open system database: %v", err)
	}
	defer system.Close()

	manager := storage.NewManager(dataDir)

	issuer, err := auth.NewTokenIssuer([]byte(jwtSecret), 24*time.Hour)
	if err != nil {
		log.Fatalf("init token issuer: %v", err)
	}

	encryptor, err := auth.NewEncryptor(auth.DeriveKey(encryptionKey))
	if err != nil {
		log.Fatalf("init encryptor: %v", err)
	}

	dispatcher := webhooks.NewDispatcher()
	dispatcher.Decryptor = encryptor

	templates, err := template.ParseFS(web.FS, "templates/*.html", "templates/pages/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		log.Fatalf("load static assets: %v", err)
	}

	server := api.NewServer(system, manager, issuer, dispatcher, templates)
	server.StaticFS = staticFS
	server.Encryptor = encryptor
	if maxUploadMB := os.Getenv("TRICMS_MAX_UPLOAD_MB"); maxUploadMB != "" {
		if mb, err := strconv.ParseInt(maxUploadMB, 10, 64); err == nil && mb > 0 {
			server.MaxUploadSize = mb << 20
		} else {
			log.Printf("WARNING: ignoring invalid TRICMS_MAX_UPLOAD_MB=%q", maxUploadMB)
		}
	}
	defer server.Close()

	if err := bootstrapFirstAdmin(system); err != nil {
		log.Fatalf("bootstrap admin account: %v", err)
	}

	router := api.NewRouter(server)

	log.Printf("triCMS listening on %s (data dir: %s)", addr, dataDir)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// bootstrapFirstAdmin creates a global ADMIN account on first run (empty
// users table) so the instance isn't unreachable out of the box. The
// generated password is printed once and must be changed after first login.
func bootstrapFirstAdmin(system *storage.SystemDB) error {
	ctx := context.Background()
	users, err := system.ListUsers(ctx)
	if err != nil {
		return err
	}
	if len(users) > 0 {
		return nil
	}

	email := envOr("TRICMS_BOOTSTRAP_EMAIL", "admin@tricms.local")
	password, err := randomSecret()
	if err != nil {
		return err
	}
	password = password[:16]

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	u := &storage.User{ID: uuid.NewString(), Email: email, PasswordHash: hash, IsGlobalAdmin: true}
	if err := system.CreateUser(ctx, u); err != nil {
		return err
	}

	log.Println("========================================================")
	log.Println("No users found: created the first global ADMIN account.")
	log.Printf("  email:    %s\n", email)
	log.Printf("  password: %s\n", password)
	log.Println("Change this password immediately after logging in.")
	log.Println("========================================================")
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
