package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	benchFlag := flag.Bool("bench", true, "Run benchmark suite")
	flag.Parse()

	if *benchFlag {
		runBenchmarkSuite()
		return
	}

	fmt.Println("Cheesepath CLI: high-performance Go alternative to LangChain and LangGraph.")
	fmt.Println("Use -bench to run the zero-network benchmark suite.")
	os.Exit(0)
}
