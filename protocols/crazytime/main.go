package main

import (
	"fmt"
	"time"
)

func main() {
	time1 := time.Now().Local()
	for i := 0; i <= 100; i++ {
	}
	time2 := time.Now().Local()
	fmt.Println(time1)
	fmt.Println(time2)
	fmt.Println(time2.Sub(time1))
}
