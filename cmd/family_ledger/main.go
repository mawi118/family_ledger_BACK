package main

import (
	"context"
	"log"
	"net"

	"github.com/mawi118/family_ledger_BACK/internal/config"
	"github.com/mawi118/family_ledger_BACK/internal/db"
	"github.com/mawi118/family_ledger_BACK/proto"
	"google.golang.org/grpc"
)

func main() {
	cfg, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	pool, err := db.Connect(context.Background(), cfg.DSN())
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	listener, err := net.Listen("tcp", ":5050")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	proto.RegisterHealthServer(grpcServer, proto.NewServer(pool))
	log.Printf("server listening at %v", listener.Addr())
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

}
