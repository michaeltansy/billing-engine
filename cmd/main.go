package main

import (
	"log"
	"net/http"

	"github.com/michaeltansy/billing-engine/config"
	postgres "github.com/michaeltansy/billing-engine/database/postgres"
)

func main() {
	cfg, err := config.LoadConfig("files/config/app/config.yaml")
	if err != nil {
		log.Fatalln("Err load config, err: ", err)
	}

	var connManager postgres.ConnManager
	{
		var err error
		connManager, err = postgres.NewConnManager(
			cfg.Database.Host,
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

	log.Println("Server is ready listen to port :8080")

	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalln(err)
	}
}
