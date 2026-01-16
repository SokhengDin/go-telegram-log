package config

import (
	"log"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	TelegramBotToken string
	JWTSecret        string
	ServerPort       string
	RateLimit        int
	RateLimitWindow  time.Duration
	RedisAddr        string
	RedisPassword    string
	RedisDB          int
	Workers          int
}

func Load() *Config {
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Warning: .env file not found, using environment variables")
	}

	viper.SetDefault("SERVER_PORT", "8080")
	viper.SetDefault("RATE_LIMIT", 100)
	viper.SetDefault("RATE_LIMIT_WINDOW", 60)
	viper.SetDefault("REDIS_ADDR", "localhost:6379")
	viper.SetDefault("REDIS_PASSWORD", "")
	viper.SetDefault("REDIS_DB", 0)
	viper.SetDefault("WORKERS", 10)

	return &Config{
		TelegramBotToken: viper.GetString("TELEGRAM_BOT_TOKEN"),
		JWTSecret:        viper.GetString("JWT_SECRET"),
		ServerPort:       viper.GetString("SERVER_PORT"),
		RateLimit:        viper.GetInt("RATE_LIMIT"),
		RateLimitWindow:  time.Duration(viper.GetInt("RATE_LIMIT_WINDOW")) * time.Second,
		RedisAddr:        viper.GetString("REDIS_ADDR"),
		RedisPassword:    viper.GetString("REDIS_PASSWORD"),
		RedisDB:          viper.GetInt("REDIS_DB"),
		Workers:          viper.GetInt("WORKERS"),
	}
}
