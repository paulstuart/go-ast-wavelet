package util

import "fmt"

func Greet(name string) {
	for i := range 3 {
		if i == 0 {
			fmt.Printf("Hello, %s!\n", name)
		}
	}
}
