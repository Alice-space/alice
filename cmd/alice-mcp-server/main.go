package main

import "log"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		log.Fatal(err)
	}
}
