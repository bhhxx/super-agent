package tools

import (
	"os"
	"strings"
)

// secretEnvMarkers are name fragments that mark a variable as a credential.
// The parent environment is not inherited wholesale by child processes: a tool
// call as ordinary as `printenv` would otherwise hand every API key in the
// environment straight to the model.
var secretEnvMarkers = []string{
	"API_KEY",
	"APIKEY",
	"AUTH_TOKEN",
	"ACCESS_TOKEN",
	"SESSION_TOKEN",
	// Bare TOKEN covers the common one-off names (GITHUB_TOKEN, NPM_TOKEN,
	// HF_TOKEN, GITLAB_TOKEN, …) that do not spell out auth/access.
	"TOKEN",
	"SECRET",
	"PASSWORD",
	"PASSWD",
	"CREDENTIAL",
	"PRIVATE_KEY",
	// AWS access keys have no secret-shaped name apart from the key id.
	"AWS_ACCESS_KEY_ID",
	// Connection URLs commonly embed credentials inline.
	"DATABASE_URL",
	"POSTGRES_URL",
	"MYSQL_URL",
	"MONGODB_URI",
	"MONGO_URL",
	"REDIS_URL",
}

// childEnv returns the environment for tool subprocesses: the parent
// environment with credential-shaped variables removed.
//
// A denylist rather than an allowlist on purpose. Tool subprocesses legitimately
// need a wide range of variables (PATH, HOME, proxy settings, Go and git
// configuration), and an allowlist narrow enough to be safe would break builds.
// The names that must never leak are a much smaller and more stable set.
func childEnv() []string {
	parent := os.Environ()
	env := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if !found || isSecretEnvName(name) {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func isSecretEnvName(name string) bool {
	upper := strings.ToUpper(name)
	for _, marker := range secretEnvMarkers {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}
