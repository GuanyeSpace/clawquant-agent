package process

import (
	"context"
	"log"
)

type Manager struct {
	logger *log.Logger
}

func NewManager(logger *log.Logger) *Manager {
	return &Manager{logger: logger}
}

func (m *Manager) BotsRunning() int {
	return 0
}

func (m *Manager) StopAll(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if m.logger != nil {
		m.logger.Print("Stopping all child processes")
	}

	return nil
}
