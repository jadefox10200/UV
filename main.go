package main

import (
	"container/list"
	"fmt"
	"machine"
	"time"
)

//TODO: make a toml file for param uploads on board start up. possible?

var maxDiff int64 = 300
var qLenMax = 3

// pin set ups:
const PinToggle = machine.PinRising | machine.PinFalling

var lastState10 bool = true
var currState10 bool
var lastState11 bool = true
var currState11 bool
var lastState12 bool = true
var currState12 bool
var lastState13 bool = true
var currState13 bool

type Queue struct {
	//list of durations as gotten from the input sensor:
	Q         *list.List
	InputOn   chan bool
	InputOff  chan bool
	OutputOn  chan bool
	OutputOff chan bool
	// ResetChan chan bool
	Kill     chan bool
	MinMilli int64
	MaxMilli int64
	// Ticker    *time.Ticker
	UserInput chan string
}

func (q Queue) EmptyQ() {
	//empty the Q:
	qLen := q.Q.Len()
	if qLen == 0 {
		return
	}
	for i := 0; i < qLen; i++ {
		el := q.Q.Front()
		q.Q.Remove(el)
	}
}

// func (q Queue) Reset() {
// 	//empty the Q:
// 	qLen := q.Q.Len()
// 	if qLen == 0 {
// 		return
// 	}
// 	for i := 0; i < qLen; i++ {
// 		el := q.Q.Front()
// 		q.Q.Remove(el)
// 	}
// }

func NewQueue() Queue {
	q := list.New()
	p := Queue{
		Q:         q,
		InputOn:   make(chan bool, 20),
		InputOff:  make(chan bool, 20),
		OutputOn:  make(chan bool, 20),
		OutputOff: make(chan bool, 20),
		Kill:      make(chan bool, 20),
		UserInput: make(chan string),
	}

	return p
}

func compare(value *list.Element, reference int64) bool {
	//type assertion of the interface type list.Element any
	diff := reference - value.Value.(int64)
	//if the difference between the too is too great, return that they don't match
	if diff > maxDiff || diff < -maxDiff {
		fmt.Printf("Wrong Diff: %v", diff)
		return false
	}
	fmt.Printf("Diff: %v", diff)
	//return a match
	return true
}

func blinky() {
	fmt.Println("ran blinky")
	led := machine.LED
	led.Configure(machine.PinConfig{Mode: machine.PinOutput})
	led.High()
	time.Sleep(time.Second * 3)
	for {
		led.Low()
		time.Sleep(time.Millisecond * 350)
		led.High()
		time.Sleep(time.Millisecond * 350)
	}
}

func (q Queue) NewProcess() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	t := time.Now()

	for {
		select {
		case <-q.InputOff:
			//if the counter is too small we assume we didn't see a sheet:
			diff := time.Since(t)
			if diff.Milliseconds() < q.MinMilli {
				fmt.Println("saw something small")
				return
			}

			//we got a sheet and so should reset the time bomb:
			// q.Ticker.Reset(time.Second * 3)

			//as the counter was larger than 1, we send the duration onto the queue:
			q.Q.PushFront(diff.Milliseconds())
			if q.Q.Len() > qLenMax {
				fmt.Println("Max number %v sheets exceeded.\n", qLenMax)
				q.Kill <- true
				return
			}
			fmt.Printf("added dur: %v; Q-len: %v\n", diff.Milliseconds(), q.Q.Len())
			return
		case <-ticker.C:
			diff := time.Since(t)
			fmt.Printf("Milliseconds: %v\n", diff.Milliseconds())
			if diff.Milliseconds() > q.MaxMilli {
				fmt.Println("Input sensor covered too long")
				q.Kill <- true
				return
			}

		}
	}
}

func (q Queue) EndProcess() {

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	t := time.Now()

	for {
		select {
		case <-q.OutputOff:
			diff := time.Since(t)

			//if the counter is too small we assume we didn't see a sheet:
			if diff.Milliseconds() < q.MinMilli {
				fmt.Printf("Saw something small on output\n")
				return
			}

			//we got a sheet and so should reset the time bomb:
			//q.Ticker.Reset(time.Second * 3)

			if q.Q.Len() == 0 {
				fmt.Printf("nothing in Q but output got a sheet!\n")
				return
			}
			//get the first element from the queue:
			el := q.Q.Front()
			if compare(el, diff.Milliseconds()) {
				fmt.Println("Got the right one")
				q.Q.Remove(el)
				fmt.Printf("removed an el. Q Len= %v\n", q.Q.Len())
			} else {
				fmt.Println("They didn't match, something wrong!")
				q.Kill <- true
			}
			return
		case <-ticker.C:
			diff := time.Since(t)
			fmt.Printf("Milliseconds: %v\n", diff.Milliseconds())
			if diff.Milliseconds() > q.MaxMilli {
				q.Kill <- true
				return
			}

		}
	}
}

func main() {

	time.Sleep(2 * time.Second)
	fmt.Println("run init")
	//set up channels:
	//a ticker must be activated seperately:
	q := NewQueue()
	q.MinMilli = 300
	q.MaxMilli = 4000
	//simulating sheets entering the UV tunnel:
	go func() {
		// for {
		// 	time.Sleep(time.Second * 1)
		// 	q.InputOn <- true
		// 	time.Sleep(time.Second * 3)
		// 	q.InputOff <- true
		// 	fmt.Printf("Q len: %v\n", q.Q.Len())
		// }

		initButtons(q)
	}()

	go blinky()

	// go func() {
	// 	time.Sleep(time.Second * 4)
	// 	for {
	// 		time.Sleep(time.Second * 1)
	// 		q.OutputOn <- true
	// 		time.Sleep(time.Second * 3)
	// 		q.OutputOff <- true
	// 	}
	// }()

	//start the ticker and attach to Q:
	// ticker := time.NewTicker(time.Second * 3)
	// defer ticker.Stop()

	// q.Ticker = ticker
	//set up queue receiver:
	for {
		select {
		case <-q.InputOn:
			go q.NewProcess()
		case <-q.OutputOn:
			go q.EndProcess()
		// case <-q.ResetChan:
		// go q.Reset()
		//set a time bomb of 3 seconds so if the Q is greater than 1 and this is called, we kill.
		// case <-q.Ticker.C:
		// 	q.LenCheck()
		case <-q.Kill:
			q.KillFunc()
		}
	}

}

// func (q Queue) LenCheck() {
// 	//if this is called, we haven't seen sheets for 3 seconds and need to check if the Q is greater than zero.
// 	//if it is greater than 0, it means we lost a sheet:
// 	if q.Q.Len() > 0 {
// 		fmt.Printf("SHEET STUCK IN TUNNEL, QLEN = %v\n", q.Q.Len())
// 		q.Kill <- true
// 	}
// 	q.Ticker.Reset(time.Second * 3)
// 	return
//}

func (q Queue) KillFunc() {

	fmt.Println("KILL ACTIVE. SOFT RESET, PLEASE WAIT...")
	panic("KILL ME")
	// q.EmptyQ()
	//send kill signals
	time.Sleep(time.Second * 5)
	fmt.Printf("Q Len = %v, READY TO RUN\n", q.Q.Len())
	return

}

func initButtons(q Queue) {
	//10 - 13
	pin10 := machine.GPIO12
	pin11 := machine.GPIO13
	pin12 := machine.GPIO15
	pin13 := machine.GPIO14

	pin10.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	pin11.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	pin12.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	pin13.Configure(machine.PinConfig{Mode: machine.PinInputPullup})

	//INFEED SENSOR
	pin10.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed:
		//debounce to ensure noise doesn't activate.
		currState10 = p.Get()
		if lastState10 != currState10 {
			lastState10 = p.Get()
			if p.Get() == false {
				fmt.Printf("pushed inputon\n")
				q.InputOn <- true
			} else {
				fmt.Printf("released inputon\n")
				q.InputOff <- true
			}
		}

	})

	//OUTFEED SENSOR
	pin11.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed:
		currState11 = p.Get()
		if lastState11 != currState11 {
			lastState11 = p.Get()
			if p.Get() == false {
				fmt.Printf("pushed output\n")
				q.OutputOn <- true
			} else {
				fmt.Printf("output released\n")
				q.OutputOff <- true
			}
		}
	})

	//RESET BUTTON
	// pin12.SetInterrupt(machine.PinRising, func(p machine.Pin) {
	// 	//when pushed:
	// 	currState12 = p.Get()
	// 	if lastState12 != currState12 {
	// 		lastState12 = p.Get()
	// 		q.ResetChan <- true
	// 	}
	// })

	//BLANK BUTTON
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
}
