package config

// Version holds the current package build version string.
var Version = "v0.0.0"

// Config holds environment variable settings.
type Config struct {
	Time        string `env:"PB_TIME" required:"true" usage:"Scheduler time, format HH:MM e.g daily at 13:00 or periodic interval - [d]m|h|d e.g. every one hour 1h. When set to 0, one-time run."`
	DataDir     string `env:"PB_DATA_DIR" default:"/data" usage:"Where store SQLite database."`
	CFDatabase  string `env:"PB_CF_DB_ID" required:"true" usage:"ID of Cloudflare D1 database to store results."`
	CFAccountID string `env:"PB_CF_ACCOUNT_ID" required:"true" usage:"Cloudflare account ID."`
	CFD1Token   string `env:"PB_CF_D1_TOKEN" required:"true" usage:"Cloudflare API token with D1 [write] permission."`
	CacheDir    string `env:"PB_CACHE_DIR" usage:"Download source databases once, then always return the local copy (useful for dev/testing)."`
	UrlhausKey  string `env:"PB_URLHAUS_KEY" required:"true" usage:"URLhaus auth key."`
}
