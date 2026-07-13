package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/michaeltansy/billing-engine/config"
	postgres "github.com/michaeltansy/billing-engine/database/postgres"
)

func main() {
	cfg, err := config.LoadConfig(config.ConfigPath())
	if err != nil {
		log.Fatalln("Err load config, err: ", err)
	}

	var connManager postgres.ConnManager
	{
		var err error
		connManager, err = postgres.NewConnManager(
			cfg.Database.Host,
			cfg.Database.Port,
			cfg.Database.DbName,
			cfg.Database.Username,
			cfg.Database.Password)
		if err != nil {
			log.Fatalln("Postgres connection manager err: ", err)
		}
	}
	if connManager != nil {
		defer connManager.Close()
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Server is ready, listening on %s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalln(err)
	}
}
