package main

import (
	"container/list"
	"machine"
	"runtime"
	"time"
)

var maxDiff int64 = 300
var qLenMax = 3

// pin set ups:
const PinToggle = machine.PinRising | machine.PinFalling

var lastState10p bool = true
var currState10p bool = false
var lastState11p bool = true
var currState11p bool = false
var lastState12p bool = true
var currState12p bool = false
var lastState13p bool = true
var currState13p bool = false

var lastState10 *bool = &lastState10p
var currState10 *bool = &currState10p
var lastState11 *bool = &lastState11p
var currState11 *bool = &currState11p
var lastState12 *bool = &lastState12p
var currState12 *bool = &currState12p
var lastState13 *bool = &lastState13p
var currState13 *bool = &currState13p

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
	//if the difference between the two is too great, return that they don't match
	if diff > maxDiff || diff < -maxDiff {
		println("Wrong Diff: ", diff)
		return false
	}
	println("Diff: ", diff)
	//return a match
	return true
}

func blinky() {
	println("ran blinky")
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

func (q Queue) InfeedSensor() {

	for {
		//wait for the sensor to turn on:
		<-q.InputOn
		t := time.Now()
		ticker := time.NewTicker(time.Second)
		select {
		case <-q.InputOff:
			//if the counter is too small we assume we didn't see a sheet:
			diff := time.Since(t)
			if diff.Milliseconds() < q.MinMilli {
				println("saw something small")
				ticker.Stop()
				continue
			}

			//as the counter was larger than 1, we send the duration onto the queue:
			q.Q.PushFront(diff.Milliseconds())
			if q.Q.Len() > qLenMax {
				println("Max number %v sheets exceeded.\n", qLenMax)
				q.Kill <- true
				return
			}
			println("added dur: %v; Q-len: %v\n", diff.Milliseconds(), q.Q.Len())
			ticker.Stop()
		case <-ticker.C:
			diff := time.Since(t)
			println("Milliseconds: %v\n", diff.Milliseconds())
			if diff.Milliseconds() > q.MaxMilli {
				println("Input sensor covered too long")
				q.Kill <- true
				return
			}
		}
	}
}

func (q Queue) OutfeedSensor() {

	for {
		//wait for the sensor to turn on:
		<-q.OutputOn
		//once on, capture the time and start a ticker:
		t := time.Now()
		ticker := time.NewTicker(time.Second)
		select {
		//sensor turns off, stop the ticker, check the length, add to Q:
		case <-q.OutputOff:
			ticker.Stop()
			diff := time.Since(t)
			//if the counter is too small we assume we didn't see a sheet:
			if diff.Milliseconds() < q.MinMilli {
				println("Saw something small on output\n")
				continue
			}
			if q.Q.Len() == 0 {
				println("nothing in Q but output got a sheet!\n")
				continue
			}

			//get the first element from the queue:
			el := q.Q.Front()
			if compare(el, diff.Milliseconds()) {
				println("Got the right one")
				q.Q.Remove(el)
				println("removed an el. Q Len= %v\n", q.Q.Len())
			} else {
				println("They didn't match, something wrong!")
				q.Kill <- true
			}
		case <-ticker.C:
			diff := time.Since(t)
			println("Milliseconds: %v\n", diff.Milliseconds())
			if diff.Milliseconds() > q.MaxMilli {
				q.Kill <- true
				return
			}
		}
	}
}

func main() {

	time.Sleep(5 * time.Second)
	println("run init")
	// debug.SetMemoryLimit(150000)
	//set up channels:
	//a ticker must be activated seperately:
	q := NewQueue()
	q.MinMilli = 300
	q.MaxMilli = 4000

	//set up for the sensors:
	go func() {
		initPins(q)
		q.InfeedSensor()
		q.OutfeedSensor()
	}()

	go blinky()

	// start the ticker for memStats:
	stats := &runtime.MemStats{}
	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()

	//set up queue receiver:
	//now only used for periodic memStats and the KillFunc()
	for {
		select {
		case <-ticker.C:
			runtime.ReadMemStats(stats)
			println("Heap in use: ", stats.HeapInuse)
			// println("input len:", len(q.InputOn))
		case <-q.Kill:
			q.KillFunc()
		}
	}

}

func (q Queue) KillFunc() {

	println("KILL ACTIVE.")
	panic("KILL ME")
	// q.EmptyQ()
	//send kill signals
	return

}

func initPins(q Queue) {
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
		*currState10 = p.Get()
		if *lastState10 != *currState10 {
			*lastState10 = p.Get()
			if p.Get() == false {
				q.InputOn <- true
			} else {
				q.InputOff <- true
			}
		}
	})

	//OUTFEED SENSOR
	pin11.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed:
		*currState11 = p.Get()
		if *lastState11 != *currState11 {
			*lastState11 = p.Get()
			if p.Get() == false {
				q.OutputOn <- true
			} else {
				q.OutputOff <- true
			}
		}
	})

}
