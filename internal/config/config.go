package config

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/caarlos0/env"
	"github.com/sirupsen/logrus"
)

func init() {
	env.Parse(&Config)

	setupLogrus()
	if err := ValidateForcemerge(); err != nil {
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

// ValidateForcemerge rejects the force merge durations that are not a positive number of seconds. A value that is set but
// is not a whole number (or is empty) is reported with its raw text: the parsed value would be 0 and the error
// misleading. A wait of 0 would never wait for GitHub, a timeout of 0 would cancel every force merge at once.
func ValidateForcemerge() error {
	for _, v := range []struct {
		name   string
		parsed int
	}{
		{"GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS", Config.WorkflowForcemergeWaitSeconds},
		{"GOLIAC_WORKFLOW_FORCEMERGE_TIMEOUT_SECONDS", Config.WorkflowForcemergeTimeoutSeconds},
	} {
		if err := checkSeconds(v.name, v.parsed); err != nil {
			return err
		}
	}
	return nil
}

func checkSeconds(name string, parsed int) error {
	if raw, set := os.LookupEnv(name); set {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("%s=%q is not a whole number of seconds", name, raw)
		}
		parsed = n
	}
	if parsed <= 0 {
		return fmt.Errorf("%s must be a positive number of seconds (got %d)", name, parsed)
	}
	return nil
}
