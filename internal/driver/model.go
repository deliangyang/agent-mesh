package driver

import "strings"

// CLIModel returns the model name the engine CLI wants, without the engine/ prefix.
func CLIModel(id, engine string) string {
	id = strings.TrimSpace(id)
	if id == "" || id == engine {
		return ""
	}
	if engine != "" {
		prefix := engine + "/"
		if strings.HasPrefix(id, prefix) {
			return strings.TrimPrefix(id, prefix)
		}
	}
	return id
}

// WithModelFlag appends --model to CLI args. Empty returns the args unchanged.
func WithModelFlag(args []string, model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return args
	}
	return append(args, "--model", model)
}
