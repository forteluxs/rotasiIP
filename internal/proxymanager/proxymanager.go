package proxymanager

import (
	"bufio"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/mubeng/mubeng/pkg/helper"
	"github.com/mubeng/mubeng/pkg/mubeng"
)

// ProxyManager defines the proxy list and current proxy position
type ProxyManager struct {
	CurrentIndex int
	filepath     string
	allowedCCs   []string
	Length       int
	Proxies      []string
}

func init() {
	// TODO(dwisiswant0): deprecated, update this later.
	// nolint: staticcheck
	rand.Seed(time.Now().UnixNano())

	manager = &ProxyManager{CurrentIndex: -1}
}

// New initialize ProxyManager. allowedCCs optionally restricts the pool to
// proxies annotated with a matching "#CC" country-code suffix (e.g. "socks5://1.2.3.4:1080#US").
// Lines without a CC suffix are always included regardless of allowedCCs.
func New(filename string, allowedCCs []string) (*ProxyManager, error) {
	keys := make(map[string]bool)

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	manager.Proxies = []string{}
	manager.filepath = filename
	manager.allowedCCs = allowedCCs

	ccSet := make(map[string]bool, len(allowedCCs))
	for _, cc := range allowedCCs {
		ccSet[strings.ToUpper(strings.TrimSpace(cc))] = true
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse optional trailing #CC country-code annotation
		cc := ""
		if idx := strings.LastIndex(line, "#"); idx != -1 {
			suffix := strings.ToUpper(strings.TrimSpace(line[idx+1:]))
			// Only treat as CC if it's exactly 2 alpha chars
			if len(suffix) == 2 && isAlpha(suffix) {
				cc = suffix
				line = strings.TrimSpace(line[:idx])
			}
		}

		// Apply geo filter: skip only if we have allowed CCs AND the proxy
		// has a CC annotation that doesn't match.
		if len(ccSet) > 0 && cc != "" && !ccSet[cc] {
			continue
		}

		evalProxy := helper.Eval(line)
		strippedProxy := placeholder.ReplaceAllString(evalProxy, "")
		if _, exists := keys[strippedProxy]; !exists {
			_, err = mubeng.Transport(strippedProxy)
			if err == nil || errors.Is(err, mubeng.ErrSwitchTransportAWSProtocolScheme) {
				keys[strippedProxy] = true
				manager.Proxies = append(manager.Proxies, evalProxy)
			}
		}
	}

	manager.Count()

	if manager.Length < 1 {
		return manager, fmt.Errorf("open %s: has no valid proxy URLs", filename)
	}

	return manager, scanner.Err()
}

func isAlpha(s string) bool {
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
