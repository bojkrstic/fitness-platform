package bootstrap

import (
	"context"
	"fmt"
	"log"

	"fitnes-platform/internal/config"
	"fitnes-platform/internal/handler"
	"fitnes-platform/internal/repository"
	"fitnes-platform/internal/service"
)

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := context.Background()
	db, err := repository.OpenDatabase(ctx, cfg.DBURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	store := repository.NewPostgresStore(db)
	svc := service.New(store, cfg)
	httpHandler := handler.NewHttpHandler(svc)

	if err := httpHandler.Init(ctx); err != nil {
		_ = store.Close()
		return err
	}
	defer func() {
		if err := httpHandler.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	return httpHandler.Serve(cfg.Port)
}
