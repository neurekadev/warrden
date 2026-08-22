package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRetryCount    = 3
	defaultTimeoutSecond = 30
)

// Options contains process-level environment configuration.
type Options struct {
	DryRun         bool
	Timezone       string
	AppVersion     string
	AppVersionSet  bool
	DatabasePath   string
	ConfigPath     string
	RetryCount     int
	AttemptTimeout time.Duration
}

// OptionsFromEnv reads the established wArrden environment contract.
func OptionsFromEnv() Options {
	databasePath, databasePathSet := os.LookupEnv("DATABASE_PATH")
	if !databasePathSet {
		databasePath = "data/warden.db"
	}
	configPath, configPathSet := os.LookupEnv("CONFIG_PATH")
	if !configPathSet {
		configPath = "data/config.yaml"
	}

	retryCount := defaultRetryCount
	if parsed, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("HTTP_RETRY_COUNT")), 10, 32); err == nil && parsed >= 0 {
		retryCount = int(parsed)
	}
	timeoutSeconds := defaultTimeoutSecond
	if parsed, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("HTTP_TIMEOUT_SECONDS")), 10, 32); err == nil && parsed > 0 {
		timeoutSeconds = int(parsed)
	}
	appVersion, appVersionSet := os.LookupEnv("APP_VERSION")
	if !appVersionSet {
		appVersion, appVersionSet = os.LookupEnv("GIT_TAG")
	}

	return Options{
		DryRun:   strings.EqualFold(os.Getenv("DRY_RUN"), "true"),
		Timezone: os.Getenv("TZ"), AppVersion: appVersion, AppVersionSet: appVersionSet,
		DatabasePath: databasePath, ConfigPath: configPath, RetryCount: retryCount,
		AttemptTimeout: time.Duration(timeoutSeconds) * time.Second,
	}
}
