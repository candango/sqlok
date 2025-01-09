package main

import (
	"fmt"
	"os"

	"github.com/namsral/flag"
)

var Version string

type flags struct {
	Watch bool
}

func parseFlags() flags {
	var flags flags

	flagset := flag.NewFlagSetWithEnvPrefix(os.Args[0], "SQLOK",
		flag.ExitOnError)

	flagset.Usage = func() {
		fmt.Fprintf(os.Stderr, "Candango SqlOk %s\nUsage of %s:\n",
			Version, os.Args[0])
		flagset.PrintDefaults()
	}

	flagset.BoolVar(&flags.Watch, "watch", false, "watch if files were changed and restart")

	flagset.Parse(os.Args[1:])

	return flags
}
