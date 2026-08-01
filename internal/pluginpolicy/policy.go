package pluginpolicy

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

const DisablePluginsEnv = "CLIPROXY_DISABLE_PLUGINS"

var (
	ErrDisabled     = errors.New("plugin capability disabled by CLIPROXY_DISABLE_PLUGINS")
	ErrInvalidValue = errors.New("invalid CLIPROXY_DISABLE_PLUGINS value")
	warnedValues    sync.Map
)

func ParseDisabled(raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	disabled, errParse := strconv.ParseBool(raw)
	if errParse != nil {
		return true, fmt.Errorf("%w: %q", ErrInvalidValue, raw)
	}
	return disabled, nil
}

func Disabled() bool {
	raw := os.Getenv(DisablePluginsEnv)
	disabled, errPolicy := ParseDisabled(raw)
	if errPolicy != nil {
		if _, loaded := warnedValues.LoadOrStore(raw, struct{}{}); !loaded {
			log.WithError(errPolicy).Warn("invalid plugin lockdown policy; plugins will remain disabled")
		}
		return true
	}
	return disabled
}
