package main

import (
	"flag"
	"log"
	"os"
	"strings"
)

const (
	self = "d64"
)

var (
	flagVolume = flag.String("v", "", "volume name for new d64 file")
)

func usage() {
	log.Printf("usage: %s [Create/eXtract/Help]", self)
	os.Exit(2)
}

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
	}

	var code int
	subcmd := os.Args[1]
	switch strings.ToLower(subcmd) {
	case "c", "create":
		code = create(os.Args[2:])
	case "x", "extract":
		code = extract(os.Args[2:])
	default:
		usage()
	}
	os.Exit(code)
}
