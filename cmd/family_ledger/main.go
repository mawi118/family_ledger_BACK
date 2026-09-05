package main

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/mawi118/family_ledger_BACK/internal/auth"
	"github.com/mawi118/family_ledger_BACK/internal/config"
	"github.com/mawi118/family_ledger_BACK/internal/db"
	"github.com/mawi118/family_ledger_BACK/internal/health"
	"github.com/mawi118/family_ledger_BACK/internal/interceptor"
	"github.com/mawi118/family_ledger_BACK/proto"
	"google.golang.org/grpc"
)

func main() {
	//парсим файл конфига
	cfg, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	//создаем пул соединений
	pool, err := db.Connect(context.Background(), cfg.DSN())
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close() //сразу отложенно закрываем

	//открываем порт 5050 на запросы
	listener, err := net.Listen("tcp", ":5050")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	//создали сервер gRPC
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(interceptor.ValidationInterceptor))

	//ниже регестрируются сервера
	proto.RegisterAuthServer(grpcServer, auth.NewServer(
		pool,
		[]byte(cfg.JWT.Secret),
		time.Duration(cfg.JWT.TTLMinutes)*time.Minute),
	)
	proto.RegisterHealthServer(grpcServer, health.NewServer(pool))
	log.Printf("server listening at %v", listener.Addr())

	//(след. строка будет блокирующей - grpcServer.Serve обрабатывает запросы)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

}
