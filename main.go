package main

import (
	"container/list"
	"machine"
	"runtime"
	"time"
)

var maxDiff int64 = 300
var qLenMax = 3
var timerDuration time.Duration = 4

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
			println("Timer fired\n")
			if q.Q.Len() == 0 {
				println("Timer fired even though Q.Len() was 0. Should never happen")
			}
			q.Kill <- true
		case <-q.SheetRemovedChan:
			q.Timer.Reset(timerDuration * time.Second)
			println("Timer reset")
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

func InfeedSensor(q *Queue) {

	ticker := time.NewTicker(time.Second)
	var currTime time.Time
	var t *time.Time = &currTime
	var diffTime time.Duration
	var diff *time.Duration = &diffTime
wait:
	for {
		//wait for the sensor to turn on:
		<-q.InputOn
		*t = time.Now()
		for {
			select {
			case <-q.InputOff:
				//if the counter is too small we assume we didn't see a sheet:
				*diff = time.Since(*t)
				*t = time.Now()
				if diff.Milliseconds() > q.MinMilli {
					//if the Q is 0, and we are adding a sheet, we need to start a timer to ensure
					//no sheets get stuck under the tunnel for longer than 4 seconds (or as determined by timerDuration)
					//when the Q.Len returns to zero, the timer is returned (ie ended) each time so it must be started
					//again newly each time the Q.Len becomes greater than 0. We ASSUME that since Q.Len is zero, that there
					//are no other timers running and so must start one.
					if q.Q.Len() == 0 {
						go q.StartTimer()
						println("Timer started")
					}

					//as the counter was larger than 1, we send the duration onto the queue:
					q.Q.PushFront(diff.Milliseconds())
					if q.Q.Len() > qLenMax {
						println("Max number %v sheets exceeded.", qLenMax)
						q.Kill <- true
					}
					println("added dur & Q-len: ", diff.Milliseconds(), q.Q.Len())
				} else {
					println("saw something small on infeed")
				}
				continue wait
			case <-ticker.C:
				*diff = time.Since(*t)
				// println("Milliseconds: ", diff.Milliseconds())
				if diff.Milliseconds() > q.MaxMilli {
					println("Input sensor covered too long")
					q.Kill <- true
					return
				}
			}
		}
	}
}

func OutfeedSensor(q *Queue) {

	ticker := time.NewTicker(time.Second)
	var currTime time.Time
	var t *time.Time = &currTime
	var diffTime time.Duration
	var diff *time.Duration = &diffTime
wait:
	for {
		//wait for the sensor to turn on:
		<-q.OutputOn
		//once on, capture the time:
		*t = time.Now()
		for {
			select {
			//sensor turns off, check the length, add to Q:
			case <-q.OutputOff:
				*diff = time.Since(*t)
				//check that the counter was big enough for a sheet:
				if diff.Milliseconds() > q.MinMilli {
					if q.Q.Len() == 0 {
						println("nothing in Q but output got a sheet!")
						continue wait
					}
					//get the first element from the queue:
					el := q.Q.Front()
					if compare(el, diff.Milliseconds()) {
						println("Got the right one")
						q.Q.Remove(el)
						println("removed an el. Q Len= ", q.Q.Len())
						//if the Queue is empty we should stop the timer as nothing is in the tunnel.
						//else the Queue is not empty, we should reset the timer as we did just exit a sheet.
						//Therefore, nothing is stuck as sheets are exiting as expected
						if q.Q.Len() == 0 {
							q.StopTimer()
						} else {
							q.SheetRemovedChan <- true
						}
					} else {
						println("They didn't match, something wrong!")
						q.Kill <- true
					}
				} else {
					println("saw something small on outfeed")
				}
				continue wait
			case <-ticker.C:
				*diff = time.Since(*t)
				if diff.Milliseconds() > q.MaxMilli {
					q.Kill <- true
					return
				}
			}
		}
	}
}

func main() {

	time.Sleep(5 * time.Second)
	println("run init")
	//set up channels:
	//a ticker must be activated seperately:
	q := NewQueue()
	q.MinMilli = 300
	q.MaxMilli = 4000

	//set up for the sensors:
	go initPins(q)
	go InfeedSensor(&q)
	go OutfeedSensor(&q)
	go blinky()

	// start the ticker for memStats:
	stats := &runtime.MemStats{}
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	//set up queue receiver:
	//now only used for periodic memStats and the KillFunc()
	//check to see if the heap is larger than 1000000 and if so, force a GC just in case.
	//with the optimizations, this shouldn't be needed as much.
	for {
		select {
		case <-ticker.C:
			runtime.ReadMemStats(stats)
			println("Heap in use: ", stats.HeapInuse)
			if stats.HeapInuse > 100000 {
				runtime.GC()
			}
		case <-q.Kill:
			q.KillFunc()
		}
	}
}

// func (q Queue) EmptyQ() {
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

func compare(value *list.Element, reference int64) bool {
	//type assertion of the interface type list.Element any
	diff := reference - value.Value.(int64)
	//if the difference between the two is too great, return that they don't match
	if diff > maxDiff || diff < -maxDiff {
		println("Wrong Diff:", diff)
		return false
	}
	println("Diff: %v", diff)
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
