package main

import (
	"fmt"
	"os"
)

// log function for logging to stderr
func LogF(format string, args ...interface{}) {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	} else {
		fmt.Fprintln(os.Stderr, format)
	}
}
