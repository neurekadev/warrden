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
	RetryCount     int
	AttemptTimeout time.Duration
}

// OptionsFromEnv reads the established wArrden environment contract.
func OptionsFromEnv() Options {
	retryCount := defaultRetryCount
	if parsed, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("HTTP_RETRY_COUNT")), 10, 32); err == nil && parsed >= 0 {
		retryCount = int(parsed)
	}
	timeoutSeconds := defaultTimeoutSecond
	if parsed, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("HTTP_TIMEOUT_SECONDS")), 10, 32); err == nil && parsed > 0 {
		timeoutSeconds = int(parsed)
	}
	appVersion := strings.TrimSpace(os.Getenv("GIT_TAG"))
	if appVersion == "" {
		appVersion = "dev"
	}

	return Options{
		DryRun:   strings.EqualFold(os.Getenv("DRY_RUN"), "true"),
		Timezone: os.Getenv("TZ"), AppVersion: appVersion, RetryCount: retryCount,
		AttemptTimeout: time.Duration(timeoutSeconds) * time.Second,
	}
}
