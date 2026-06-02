package app

import "super-agent/app/instructions"

func LoadProjectInstructions(dir string) (string, error) {
	bundle, err := instructions.Load(dir)
	return bundle.Content, err
}
