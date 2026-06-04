package main

import "fmt"

func SimpleFunction() {
	fmt.Println("I am quite straightforward.")
}

func GnarlyFunction(items []int) {
	for _, item := range items {
		if item > 0 {
			switch item {
			case 42:
				go func() {
					if true {
						fmt.Println("Deeply nested complexity spike!")
					}
				}()
			default:
				fmt.Println(item)
			}
		}
	}
}

type Worker struct {
	jobs chan int
	done chan struct{}
}

func (w *Worker) Run() {
	defer close(w.done)
	for job := range w.jobs {
		select {
		case <-w.done:
			return
		default:
			w.process(job)
		}
	}
}

func (w *Worker) process(job int) {
	fmt.Println(job)
}
