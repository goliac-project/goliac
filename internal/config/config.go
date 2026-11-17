package config

import (
	"context"
	"fmt"
	"os"

	"github.com/caarlos0/env"
	"github.com/sirupsen/logrus"
)

func init() {
	env.Parse(&Config)

	setupLogrus()
	if err := ValidateForcemerge(Config.WorkflowForcemergeWaitSeconds, Config.WorkflowForcemergeTimeoutSeconds); err != nil {
		logrus.Fatalf("invalid configuration: %v", err)
	}
	if Config.OpenTelemetryEnabled {
		err := setupTraceProvider(context.Background())
		if err != nil {
			panic(err)
		}
	}
}

func setupLogrus() {
	l, err := logrus.ParseLevel(Config.LogrusLevel)
	if err != nil {
		logrus.WithField("err", err).Fatalf("failed to set logrus level:%s", Config.LogrusLevel)
	}
	logrus.SetLevel(l)
	logrus.SetOutput(os.Stdout)
	switch Config.LogrusFormat {
	case "text":
		logrus.SetFormatter(&logrus.TextFormatter{})
	case "json":
		logrus.SetFormatter(&logrus.JSONFormatter{})
	default:
		logrus.Warnf("unexpected logrus format: %s, should be one of: text, json", Config.LogrusFormat)
	}
}

// ValidateForcemerge rejects the non positive force merge durations (a wait of 0 would never wait for GitHub, and a timeout
// of 0 would cancel every force merge at once)
func ValidateForcemerge(waitSeconds, timeoutSeconds int) error {
	if waitSeconds <= 0 {
		return fmt.Errorf("GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS must be a positive number of seconds (got %d)", waitSeconds)
	}
	if timeoutSeconds <= 0 {
		return fmt.Errorf("GOLIAC_WORKFLOW_FORCEMERGE_TIMEOUT_SECONDS must be a positive number of seconds (got %d)", timeoutSeconds)
	}
	return nil
}
