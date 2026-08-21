package main

import (
	"fmt"
	"os"
)

func Infof(format string, args ...any) {
	fmt.Fprintf(os.Stdout, "\033[36m::\033[0m "+format+"\n", args...)
}

func Errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\033[31m!!\033[0m "+format+"\n", args...)
}

func Subf(format string, args ...any) {
	fmt.Fprintf(os.Stdout, "   \033[90m-> "+format+"\033[0m\n", args...)
}
