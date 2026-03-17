package command

import "encoding/json"

type CommandHandler interface {
	HandleCreateBot(cmd CreateBotCommand) error
	HandleStopBot(cmd StopBotCommand) error
	HandleRestartBot(cmd RestartBotCommand) error
}

type ExchangeConfig struct {
	Type            string `json:"type"`
	EncryptedAPIKey string `json:"encrypted_api_key"`
	EncryptedSecret string `json:"encrypted_secret"`
	TradingPair     string `json:"trading_pair"`
}

type CreateBotCommand struct {
	Type         string          `json:"type"`
	BotID        string          `json:"bot_id"`
	StrategyCode string          `json:"strategy_code"`
	Params       json.RawMessage `json:"params"`
	Exchange     ExchangeConfig  `json:"exchange"`
}

type StopBotCommand struct {
	Type  string `json:"type"`
	BotID string `json:"bot_id"`
}

type RestartBotCommand struct {
	Type  string `json:"type"`
	BotID string `json:"bot_id"`
}
