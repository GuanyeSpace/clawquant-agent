package command

import (
	"fmt"
	"log"
	"sync"
)

type Sender interface {
	Send(msg []byte) error
	SendBotStatus(botID, status, errMsg string) error
}

type Dispatcher struct {
	logger *log.Logger

	mu         sync.RWMutex
	sender     Sender
	controller BotController
}

func NewDispatcher(logger *log.Logger) *Dispatcher {
	return &Dispatcher{logger: logger}
}

func (d *Dispatcher) SetSender(sender Sender) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.sender = sender
}

func (d *Dispatcher) SetController(controller BotController) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.controller = controller
}

func (d *Dispatcher) HandleCreateBot(cmd CreateBotCommand) error {
	if d.logger != nil {
		d.logger.Printf("Received create_bot for bot_id=%s", cmd.BotID)
	}

	d.mu.RLock()
	controller := d.controller
	d.mu.RUnlock()

	if controller == nil {
		return d.sendStatus(cmd.BotID, "running", "")
	}

	if err := controller.StartBot(cmd); err != nil {
		_ = d.sendStatus(cmd.BotID, "error", err.Error())
		return err
	}

	return nil
}

func (d *Dispatcher) HandleStopBot(cmd StopBotCommand) error {
	if d.logger != nil {
		d.logger.Printf("Received stop_bot for bot_id=%s", cmd.BotID)
	}

	d.mu.RLock()
	controller := d.controller
	d.mu.RUnlock()

	if controller == nil {
		return d.sendStatus(cmd.BotID, "stopped", "")
	}

	if err := controller.StopBot(cmd.BotID); err != nil {
		_ = d.sendStatus(cmd.BotID, "error", err.Error())
		return err
	}

	return nil
}

func (d *Dispatcher) HandleRestartBot(cmd RestartBotCommand) error {
	if d.logger != nil {
		d.logger.Printf("Received restart_bot for bot_id=%s", cmd.BotID)
	}

	d.mu.RLock()
	controller := d.controller
	d.mu.RUnlock()

	if controller == nil {
		return d.sendStatus(cmd.BotID, "running", "")
	}

	if err := controller.RestartBot(cmd.BotID); err != nil {
		_ = d.sendStatus(cmd.BotID, "error", err.Error())
		return err
	}

	return nil
}

func (d *Dispatcher) sendStatus(botID, status, errMsg string) error {
	d.mu.RLock()
	sender := d.sender
	d.mu.RUnlock()

	if sender == nil {
		return fmt.Errorf("status sender not configured")
	}

	return sender.SendBotStatus(botID, status, errMsg)
}
