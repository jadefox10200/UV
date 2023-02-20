package main

import (
	"fmt"
	"machine"
	"time"
)

const PinToggle = machine.PinRising | machine.PinFalling

var lastState10 bool = true
var currState10 bool
var lastState11 bool = true
var currState11 bool
var lastState12 bool = true
var currState12 bool
var lastState13 bool = true
var currState13 bool

func main() {

	//10 - 13
	pin10 := machine.GPIO10
	pin11 := machine.GPIO11
	pin12 := machine.GPIO12
	pin13 := machine.GPIO13

	pin10.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	pin11.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	pin12.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	pin13.Configure(machine.PinConfig{Mode: machine.PinInputPullup})

	// pin10.SetInterrupt(machine.PinFalling, func(p machine.Pin) {
	// 	//when pushed:
	// 	fmt.Println("pin10 pushed")
	// })
	pin10.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed:
		//debounce to ensure noise doesn't activate.
		currState10 = p.Get()
		if lastState10 != currState10 {
			lastState10 = p.Get()
			if p.Get() == false {
				fmt.Printf("pushed 10\n")
			} else {
				fmt.Printf("released 10\n")
			}
		}

	})
	pin11.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed:
		currState11 = p.Get()
		if lastState11 != currState11 {
			lastState11 = p.Get()
			if p.Get() == false {
				fmt.Printf("pushed 11\n")
			} else {
				fmt.Printf("released 11\n")
			}
		}
	})

	pin12.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed:
		currState12 = p.Get()
		if lastState12 != currState12 {
			lastState12 = p.Get()
			if p.Get() == false {
				fmt.Printf("pushed 12\n")
			} else {
				fmt.Printf("released 12\n")
			}
		}
	})

	pin13.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed:
		currState13 = p.Get()
		if lastState13 != currState13 {
			lastState13 = p.Get()
			if p.Get() == false {
				fmt.Printf("pushed 13\n")
			} else {
				fmt.Printf("released 13\n")
			}
		}

	})

	for {
		time.Sleep(time.Second * 2)
	}

	return
}
