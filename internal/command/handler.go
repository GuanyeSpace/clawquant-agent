package command

import "encoding/json"

type CommandHandler interface {
	HandleCreateBot(cmd CreateBotCommand) error
	HandleStopBot(cmd StopBotCommand) error
	HandleRestartBot(cmd RestartBotCommand) error
}

type BotController interface {
	StartBot(cmd CreateBotCommand) error
	StopBot(botID string) error
	RestartBot(botID string) error
}

type ExchangeConfig struct {
	Type            string `json:"type"`
	APIKey          string `json:"api_key"`
	Secret          string `json:"secret"`
	EncryptedAPIKey string `json:"encrypted_api_key"`
	EncryptedSecret string `json:"encrypted_secret"`
	TradingPair     string `json:"trading_pair"`
	Testnet         bool   `json:"testnet"`
}

type CreateBotCommand struct {
	Type          string          `json:"type"`
	BotID         string          `json:"bot_id"`
	StrategyCode  string          `json:"strategy_code"`
	Params        json.RawMessage `json:"params"`
	EncryptionKey string          `json:"encryption_key,omitempty"`
	Exchange      ExchangeConfig  `json:"exchange"`
}

type StopBotCommand struct {
	Type  string `json:"type"`
	BotID string `json:"bot_id"`
}

type RestartBotCommand struct {
	Type  string `json:"type"`
	BotID string `json:"bot_id"`
}
