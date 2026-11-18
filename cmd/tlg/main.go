package main

import (
	"os"

	"taskledger/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
