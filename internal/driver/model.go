package driver

import "strings"

// CLIModel 取出引擎 CLI 要的模型名，去掉 engine/ 前缀。
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

// WithModelFlag 在 CLI 参数末尾追加 --model。空则原样返回。
func WithModelFlag(args []string, model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return args
	}
	return append(args, "--model", model)
}
