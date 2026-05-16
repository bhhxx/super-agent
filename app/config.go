package app

import "os"

type Flags struct {
	YOLO    bool
	NoTools bool
}

type Config struct {
	Provider string
	YOLO     bool
	NoTools  bool
}

func LoadConfig(flags Flags, lookup func(string) (string, bool)) Config {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	provider, _ := lookup("LLM_PROVIDER")
	return Config{
		Provider: provider,
		YOLO:     flags.YOLO || envTrue(lookup, "YOLO"),
		NoTools:  flags.NoTools || envTrue(lookup, "NO_TOOLS"),
	}
}

func envTrue(lookup func(string) (string, bool), key string) bool {
	value, ok := lookup(key)
	return ok && value == "true"
}
