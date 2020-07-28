package logger

import (
	"os"
	"fmt"
)

func PrintMsg(s string, msg ...interface{}) {
	fmt.Fprintf(os.Stdout, s + "\n" , msg...)
}

func PrintErr(s string, msg ...interface{}) {
	fmt.Fprintf(os.Stderr, s + "\n" , msg...)
}
