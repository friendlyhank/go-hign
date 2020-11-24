package main

import (
	"cmd/go/internal/base"
	"cmd/go/internal/run"
	"flag"
)

func init(){
	base.Go.Commands = []*base.Command{
		run.CmdRun,
	}
}

func main(){
	flag.Parse()

	args := flag.Args()
	if len(args) <1{

	}

	for bigCmd := base.Go; ; {
		for _,cmd := range bigCmd.Commands{
			if cmd.Name() != args[0]{
				continue
			}
			if !cmd.Runnable(){
				continue
			}
		}
	}
}


