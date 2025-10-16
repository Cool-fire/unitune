package main

import (
	"github.com/Cool-fire/unitune/pkg/cmd"
	"github.com/Cool-fire/unitune/pkg/cmd/unitune"
)

func main() {
	err := unitune.NewCommand().Execute()
	cmd.CheckError(err)
}
