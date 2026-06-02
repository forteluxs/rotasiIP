package helper

import (
	"os"
	"strings"
)

func getEnviron() map[string]interface{} {
	m = make(map[string]interface{})

	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		if len(pair) < 2 {
			continue
		}

		m[pair[0]] = pair[1]
	}

	return m
}
