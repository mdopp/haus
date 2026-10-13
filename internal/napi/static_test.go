package napi

import (
	"context"
	"strings"
)

// StaticValidator accepts a fixed set of tokens; tests only.
func StaticValidator(tokens []string) Validator {
	set := map[string]bool{}
	for _, t := range tokens {
		if t = strings.TrimSpace(t); t != "" {
			set[t] = true
		}
	}
	return func(_ context.Context, bearer string) bool { return set[bearer] }
}
