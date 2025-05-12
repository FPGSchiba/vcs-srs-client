package app

import (
	"context"
	"fmt"
	"go.uber.org/zap"
)

// App struct
type App struct {
	ctx    context.Context
	logger *zap.Logger
}

// NewApp creates a new App application struct
func NewApp(logger *zap.Logger) *App {
	return &App{
		logger: logger,
	}
}

// Startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
