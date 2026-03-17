package command

import (
	"errors"
	"io"
	"log"
	"testing"
)

func TestDispatcherDelegatesToController(t *testing.T) {
	controller := &fakeController{}
	dispatcher := NewDispatcher(log.New(io.Discard, "", 0))
	dispatcher.SetController(controller)

	create := CreateBotCommand{BotID: "bot-1"}
	stop := StopBotCommand{BotID: "bot-1"}
	restart := RestartBotCommand{BotID: "bot-1"}

	if err := dispatcher.HandleCreateBot(create); err != nil {
		t.Fatalf("HandleCreateBot returned error: %v", err)
	}
	if err := dispatcher.HandleStopBot(stop); err != nil {
		t.Fatalf("HandleStopBot returned error: %v", err)
	}
	if err := dispatcher.HandleRestartBot(restart); err != nil {
		t.Fatalf("HandleRestartBot returned error: %v", err)
	}

	if controller.started != "bot-1" || controller.stopped != "bot-1" || controller.restarted != "bot-1" {
		t.Fatalf("unexpected controller calls: %+v", controller)
	}
}

func TestDispatcherReportsControllerError(t *testing.T) {
	controller := &fakeController{startErr: errors.New("boom")}
	sender := &fakeSender{}
	dispatcher := NewDispatcher(log.New(io.Discard, "", 0))
	dispatcher.SetController(controller)
	dispatcher.SetSender(sender)

	err := dispatcher.HandleCreateBot(CreateBotCommand{BotID: "bot-2"})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}

	if sender.status != "error" || sender.errMsg != "boom" {
		t.Fatalf("unexpected sender call: %+v", sender)
	}
}

type fakeController struct {
	started   string
	stopped   string
	restarted string
	startErr  error
}

func (f *fakeController) StartBot(cmd CreateBotCommand) error {
	f.started = cmd.BotID
	return f.startErr
}

func (f *fakeController) StopBot(botID string) error {
	f.stopped = botID
	return nil
}

func (f *fakeController) RestartBot(botID string) error {
	f.restarted = botID
	return nil
}

type fakeSender struct {
	status string
	errMsg string
}

func (f *fakeSender) Send([]byte) error { return nil }

func (f *fakeSender) SendBotStatus(botID, status, errMsg string) error {
	f.status = status
	f.errMsg = errMsg
	return nil
}
