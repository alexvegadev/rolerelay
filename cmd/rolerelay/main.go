package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rolerelay/internal/config"
	"rolerelay/internal/credstore"
	"rolerelay/internal/db"
	"rolerelay/internal/mailer"
	"rolerelay/internal/server"
	"rolerelay/internal/store"
	"rolerelay/internal/worker"
)

var version = "0.1.0"

const defaultListenAddr = "127.0.0.1:41730"

func main() {
	var (
		addrFlag      = flag.String("addr", defaultListenAddr, "Loopback listen address (host:port)")
		portableFlag  = flag.Bool("portable", false, "Store data/app.sqlite alongside the executable")
		dbPathFlag    = flag.String("db", "", "Explicit SQLite database file path")
		noBrowserFlag = flag.Bool("no-browser", false, "Do not launch the default browser on startup")
		versionFlag   = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *versionFlag {
		fmt.Printf("RoleRelay %s\n", version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	paths, err := config.ResolveStoragePaths(config.ResolveOptions{
		ForcePortable:  *portableFlag,
		ExplicitDBPath: *dbPathFlag,
	})
	if err != nil {
		log.Fatalf("resolve storage paths: %v", err)
	}
	if err := config.EnsureDataDir(paths); err != nil {
		log.Fatalf("prepare data directory: %v", err)
	}

	conn, err := db.Open(ctx, paths.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	schemaVer, err := db.SchemaVersion(ctx, conn)
	if err != nil {
		_ = conn.Close()
		log.Fatalf("read schema version: %v", err)
	}

	repo := store.NewWithPath(conn, paths.DBPath)
	defer func() {
		if err := repo.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()
	if _, err := repo.GetOrCreateProfile(ctx); err != nil {
		log.Fatalf("initialize default profile: %v", err)
	}

	creds := credstore.NewDefaultStore()
	smtpMailer := mailer.NewSMTPMailer()
	bgWorker := worker.NewWithDeps(worker.Dependencies{
		Store:     repo,
		CredStore: creds,
		Mailer:    smtpMailer,
		Interval:  3 * time.Second,
	})
	bgWorker.Start(ctx)

	listener, actualAddr, err := listenLoopback(*addrFlag)
	if err != nil {
		log.Fatalf("listen on loopback: %v", err)
	}

	appURL := "http://" + actualAddr
	apiServer := server.New(server.Options{
		Version:       version,
		StoragePaths:  paths,
		SchemaVersion: schemaVer,
		Address:       actualAddr,
		Store:         repo,
		Worker:        bgWorker,
		CredStore:     creds,
		Mailer:        smtpMailer,
		OnExit:        stop,
	})

	httpServer := &http.Server{
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		log.Printf("RoleRelay %s listening at %s (storage=%s db=%s)", version, appURL, paths.Mode, paths.DBPath)
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
		close(serverErrCh)
	}()

	if !*noBrowserFlag {
		if err := server.OpenBrowser(appURL); err != nil {
			log.Printf("could not open default browser automatically (%v); open %s manually", err, appURL)
		}
	}

	select {
	case err := <-serverErrCh:
		if err != nil {
			log.Fatalf("http server error: %v", err)
		}
	case <-ctx.Done():
		log.Printf("shutting down RoleRelay...")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown error: %v", err)
	}
	bgWorker.Wait()
	log.Printf("RoleRelay stopped cleanly.")
}

func listenLoopback(requested string) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", requested)
	if err == nil {
		return ln, ln.Addr().String(), nil
	}
	if requested == defaultListenAddr {
		ln, fallbackErr := net.Listen("tcp", "127.0.0.1:0")
		if fallbackErr == nil {
			return ln, ln.Addr().String(), nil
		}
	}
	return nil, "", err
}
