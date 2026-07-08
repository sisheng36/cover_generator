package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"embytool/internal/server"
	"embytool/internal/version"
)

func main() {
	addr := firstNonEmpty(
		os.Getenv("ADDR"),
		os.Getenv("PORT"),
		"8055",
	)

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("EmbyTool %s 启动中, 监听 %s", version.Get(), addr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(addr)
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

