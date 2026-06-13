package main

import (
	_ "embed"
	"os"

	"github.com/webkaz-labs/updev/internal/cmd"
)

//go:embed docs/agent/SKILL.md
var agentSkillDoc string

//go:embed docs/agent/USAGE.md
var agentUsageDoc string

func main() {
	cmd.SetAgentDocs(agentSkillDoc, agentUsageDoc)
	os.Exit(cmd.Run(os.Args[1:]))
}
