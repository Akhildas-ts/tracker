// Command tracker runs the daily habit tracker as a local web app.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"tracker/internal/stats"
	"tracker/internal/store"
	"tracker/internal/web"
)

func main() {
	addr := flag.String("addr", envOr("TRACKER_ADDR", "127.0.0.1:8080"), "address to listen on")
	dbPath := flag.String("db", envOr("TRACKER_DB", defaultDBPath()), "path to the SQLite database file")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open database %s: %v", *dbPath, err)
	}
	today := stats.Key(stats.Today(time.Now()))
	// Consistency check: Job Outreach always matches the career records.
	if err := st.ResyncOutreach(); err != nil {
		log.Fatalf("sync job outreach: %v", err)
	}
	backupDir := filepath.Join(filepath.Dir(*dbPath), "backups")
	if path, err := st.Backup(backupDir, today, 30); err != nil {
		log.Printf("warning: daily backup failed: %v", err)
	} else if path != "" {
		log.Printf("backup written to %s", path)
	}

	host, _, _ := net.SplitHostPort(*addr)
	loopback := host == "127.0.0.1" || host == "localhost" || host == "::1"
	if !loopback {
		log.Printf("warning: listening on %s — anyone who can reach this address can read and change your data", *addr)
	}
	srv, err := web.New(st, web.Options{DBPath: *dbPath, BackupDir: backupDir, LoopbackOnly: loopback})
	if err != nil {
		log.Fatalf("load templates: %v", err)
	}
	httpSrv := &http.Server{Addr: *addr, Handler: srv.Routes(), ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Printf("data file: %s", *dbPath)
		log.Printf("tracker running at http://%s  (Ctrl+C to stop)", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	// On Ctrl+C, finish in-flight requests and close the database cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Print("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpSrv.Shutdown(shutdownCtx)
	if err := st.Close(); err != nil {
		log.Printf("close database: %v", err)
	}
}

// defaultDBPath keeps data in the user's home directory, outside the repo,
// so it survives re-cloning and is never committed.
func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("data", "tracker.db")
	}
	return filepath.Join(home, ".habit-tracker", "tracker.db")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
