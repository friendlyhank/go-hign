package base

import (
	"flag"
	"strings"
)

// A Command is an implementation of a go command
// like go build or go fix.
type Command struct{
	// Run runs the command.
	// The args are the arguments after the command name.
	Run func(cmd *Command, args []string)

	// UsageLine is the one-line usage message.
	// The words between "go" and the first flag or argument in the line are taken to be the command name.
	UsageLine string

	// Short is the short description shown in the 'go help' output.
	Short string

	// Long is the long message shown in the 'go help <this-command>' output.
	Long string

	//Flag is a set of flags specific to this command.
	Flag flag.FlagSet

	// Commands lists the available commands and help topics.
	// The order here is the order in which they are printed by 'go help'.
	// Note that subcommands are in general best avoided.
	//子命令
	Commands []*Command
}

var Go =&Command{
	UsageLine:"go",
	Long: `Go is a tool for managing Go source code.`,
	// Commands initialized in package main
}

func (c *Command) LongName()string{
	name := c.UsageLine
	if i := strings.Index(name, " ["); i >= 0 {
		name = name[:i]
	}
	if name == "go" {
		return ""
	}
	return strings.TrimPrefix(name, "go ")
}

// Name returns the command's short name: the last word in the usage line before a flag or argument.
func (c *Command) Name() string {
	name := c.LongName()
	if i := strings.LastIndex(name, " "); i >= 0 {
		name = name[i+1:]
	}
	return name
}

func (c *Command)Runnable()bool{
	return c.Run != nil
}

// Usage is the usage-reporting function, filled in by package main
// but here for reference by other packages.
var Usage func()
