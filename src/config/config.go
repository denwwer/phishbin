package config

// Version holds the current package build version string.
var Version = "v0.0.0"

// Config holds environment variable settings.
type Config struct {
	Time       string `env:"PB_TIME" required:"true" usage:"Run time, format HH:MM e.g daily at 13:00 or periodic interval - [d]s|m|h|d e.g. every one hour 1h"`
	DataDir    string `env:"PB_DATA_DIR" default:"/data" usage:"Where store SQLite database"`
	CFDatabase string `env:"PB_CF_DB_ID" required:"true" usage:"ID of Cloudflare D1 database to store results"`
	CFD1Token  string `env:"PB_CF_D1_TOKEN" required:"true" usage:"Cloudflare API token with D1 [write] permission"`
	UrlhausKey string `env:"PB_URLHAUS_KEY" required:"true" usage:"URLhaus auth key"`
}
