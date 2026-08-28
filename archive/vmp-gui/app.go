package main

import (
	"context"

	"vmp-gui/backend/api"
)

// App struct
type App struct {
	ctx    context.Context
	engine *api.VMPEngine
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		engine: api.NewVMPEngine(),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.engine.Startup(ctx)
}

// GetEngine returns the engine instance for binding
func (a *App) GetEngine() *api.VMPEngine {
	return a.engine
}
