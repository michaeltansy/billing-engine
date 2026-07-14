package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/michaeltansy/billing-engine/config"
	postgres "github.com/michaeltansy/billing-engine/database/postgres"
	"github.com/michaeltansy/billing-engine/internal/clock"
	delinquencydbstore "github.com/michaeltansy/billing-engine/internal/delinquency/dbstore"
	delinquencyhandler "github.com/michaeltansy/billing-engine/internal/delinquency/handler"
	delinquencyservice "github.com/michaeltansy/billing-engine/internal/delinquency/service"
	creationdbstore "github.com/michaeltansy/billing-engine/internal/loan/dbstore"
	creationhandler "github.com/michaeltansy/billing-engine/internal/loan/handler"
	creationservice "github.com/michaeltansy/billing-engine/internal/loan/service"
	outstandingdbstore "github.com/michaeltansy/billing-engine/internal/loanoutstanding/dbstore"
	outstandinghandler "github.com/michaeltansy/billing-engine/internal/loanoutstanding/handler"
	outstandingservice "github.com/michaeltansy/billing-engine/internal/loanoutstanding/service"
	paymentdbstore "github.com/michaeltansy/billing-engine/internal/payment/dbstore"
	paymenthandler "github.com/michaeltansy/billing-engine/internal/payment/handler"
	paymentservice "github.com/michaeltansy/billing-engine/internal/payment/service"
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

	// The system clock is injected rather than reached for, so delinquency is a
	// pure function of (schedule, now) and can be tested at the day boundary.
	systemClock := clock.System{}

	outstandingStore := outstandingdbstore.NewDBStore(connManager.GetDB())
	outstandingSvc := outstandingservice.NewService(outstandingStore)
	outstandingHandler := outstandinghandler.NewHandler(outstandinghandler.Dependencies{
		LoanOutstandingSvc: outstandingSvc,
	}, outstandinghandler.WithTimeoutOptions(cfg.API["loan_outstanding"].ReqTimeoutInMS))

	delinquencyStore := delinquencydbstore.NewDBStore(connManager.GetDB())
	delinquencySvc := delinquencyservice.NewService(delinquencyStore, systemClock)
	delinquencyHandler := delinquencyhandler.NewHandler(delinquencyhandler.Dependencies{
		DelinquencySvc: delinquencySvc,
	}, delinquencyhandler.WithTimeoutOptions(cfg.API["delinquency"].ReqTimeoutInMS))

	paymentStore := paymentdbstore.NewDBStore(connManager.GetDB())
	paymentSvc := paymentservice.NewService(paymentStore, systemClock)
	paymentHandler := paymenthandler.NewHandler(paymenthandler.Dependencies{
		PaymentSvc: paymentSvc,
	}, paymenthandler.WithTimeoutOptions(cfg.API["payment"].ReqTimeoutInMS))

	creationStore := creationdbstore.NewDBStore(connManager.GetDB())
	creationSvc := creationservice.NewService(creationStore)
	creationHandler := creationhandler.NewHandler(creationhandler.Dependencies{
		LoanCreationSvc: creationSvc,
	}, creationhandler.WithTimeoutOptions(cfg.API["loan_creation"].ReqTimeoutInMS))

	mux := http.NewServeMux()

	mux.HandleFunc(creationhandler.Route, creationHandler.Handle)
	mux.HandleFunc(outstandinghandler.Route, outstandingHandler.Handle)
	mux.HandleFunc(delinquencyhandler.Route, delinquencyHandler.Handle)
	mux.HandleFunc(paymenthandler.Route, paymentHandler.Handle)

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
