package main

import (
	"container/list"
	"fmt"
	"machine"
	"time"
)

var maxDiff int64 = 300
var qLenMax = 3
var timerDuration time.Duration = 4

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

func main() {

	time.Sleep(2 * time.Second)
	fmt.Println("run init")
	//set up channels:
	//a timer must be activated seperately:
	q := NewQueue()
	q.MinMilli = 300
	q.MaxMilli = 3000
	//simulating sheets entering the UV tunnel:
	go func() {
		blinky()
		initButtons(q)
	}()

	//set up queue receiver:
	for {
		select {
		case <-q.InputOn:
			go q.NewSheet()
		case <-q.OutputOn:
			go q.RemoveSheet()
		case <-q.Kill:
			q.KillFunc()
		}
	}

}

type Queue struct {
	//list of durations as gotten from the input sensor:
	Q                *list.List
	InputOn          chan bool
	InputOff         chan bool
	OutputOn         chan bool
	OutputOff        chan bool
	StopTimerChan    chan bool
	SheetRemovedChan chan bool
	Kill             chan bool
	MinMilli         int64
	MaxMilli         int64
	Timer            *time.Timer
	UserInput        chan string
}

func (q Queue) StartTimer() {
	q.TimerFunc()
	return
}

func (q Queue) StopTimer() {
	q.StopTimerChan <- true
}

// TimerFunc starts a timer and will send to q.Kill a true signal if ever reached.
// This is intended to be started once a sheet is in the UV tunnel and Q.Len is greater than 1.
func (q Queue) TimerFunc() {
	q.Timer = time.NewTimer(timerDuration * time.Second)
	defer q.Timer.Stop()
	for {
		select {
		case <-q.Timer.C:
			fmt.Printf("Timer fired\n")
			if q.Q.Len() == 0 {
				fmt.Println("Timer fired even though Q.Len() was 0. Should never happen")
			}
			q.Kill <- true
		case <-q.SheetRemovedChan:
			q.Timer.Reset(timerDuration)
			fmt.Printf("Timer reset\n")
		case <-q.StopTimerChan:
			//simply return and the defer will handle stopping
			return
		}

	}
}

func NewQueue() Queue {
	q := list.New()
	p := Queue{
		Q:                q,
		InputOn:          make(chan bool, 20),
		InputOff:         make(chan bool, 20),
		OutputOn:         make(chan bool, 20),
		OutputOff:        make(chan bool, 20),
		Kill:             make(chan bool, 20),
		StopTimerChan:    make(chan bool, 20),
		SheetRemovedChan: make(chan bool, 20),
		UserInput:        make(chan string),
	}

	return p
}

func (q Queue) NewSheet() {
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

			//if the Q is 0, and we are adding a sheet, we need to start a timer to ensure
			//no sheets get stuck under the tunnel for longer than 4 seconds (or as determined by timerDuration)
			//when the Q.Len returns to zero, the timer is returned (ie ended) each time so it must be started
			//again newly each time the Q.Len becomes greater than 0. We ASSUME that since Q.Len is zero, that there
			//are no other timers running and so must start one.
			if q.Q.Len() == 0 {
				go q.StartTimer()
				fmt.Println("Timer started")
			} else {

			}

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
			fmt.Printf("Infeed Milliseconds: %v\n", diff.Milliseconds())
			if diff.Milliseconds() > q.MaxMilli {
				fmt.Println("Input sensor covered too long")
				q.Kill <- true
				return
			}

		}
	}
}

func (q Queue) RemoveSheet() {

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
				//if the Queue is empty we should stop the timer as nothing is in the tunnel.
				//else the Queue is not empty, we should reset the timer as we did just exit a sheet.
				//Therefore, nothing is stuck as sheets are exiting as expected
				if q.Q.Len() == 0 {
					q.StopTimer()
				} else {
					q.SheetRemovedChan <- true
				}
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
		currState12 = p.Get()
		if lastState12 != currState12 {
			lastState12 = p.Get()
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
	pin13.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed:
		currState13 = p.Get()
		if lastState13 != currState13 {
			lastState13 = p.Get()
			if p.Get() == false {
				fmt.Printf("pushed output\n")
				q.OutputOn <- true
			} else {
				fmt.Printf("output released\n")
				q.OutputOff <- true
			}
		}
	})

	//BLANK BUTTON
	// pin13.SetInterrupt(PinToggle, func(p machine.Pin) {
	// 	//when pushed:
	// 	currState13 = p.Get()
	// 	if lastState13 != currState13 {
	// 		lastState13 = p.Get()
	// 		if p.Get() == false {
	// 			fmt.Printf("pushed 13\n")
	// 		} else {
	// 			fmt.Printf("released 13\n")
	// 		}
	// 	}
	//
	// })
}

func compare(value *list.Element, reference int64) bool {
	//type assertion of the interface type list.Element any
	diff := reference - value.Value.(int64)
	//if the difference between the two is too great, return that they don't match
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
