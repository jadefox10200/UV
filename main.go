package main

import (
	"container/list"
	"machine"
	"runtime"
	"time"
	"os"
)

//Max difference of the reading between the infeed sensor and the outfeed sensor.
//In actual practice, this has always been less then 10ms (average 4ms).
var maxDiff int64 = 300

//How many sheets are we allows to have in the UV tunnel at any one time:
var qLenMax = 3

//timerDuration sets how long a sheet is allowed to be in the UV tunnel.

//TODO 2023-02-25:In reality, we should set a timerResetDuration. When a sheet first enters
//the tunnel, we allow 5 seconds for it to exit and come off the Queue. In practice
//there are usually 2 sheets in the tunnel at any given time. Once the first sheet
//is taken off the Queue, we reset the timer. However, we don't actually need another full 5
//seconds because the next sheet is already in the tunnel. This needs to be tested at full
//belt speed and the minimum belt speed. The minimum belt speed in practice is 80 on the actual
//setting on the UV coater. Going slower than this results in sheet bubbling. This needs to be tested,
//to find the optimum reset duration which would be less than 5 seconds.  Ideally, this would
//be connected to an encoder on the belt and we would adjust the duration based on the actual speed
//of the belt. Maybe another day...
var timerDuration time.Duration = 5

// We want to capture if the sensor turns on or off in a single interrupt and so capture it as a toggle.
const PinToggle = machine.PinRising | machine.PinFalling

//init the states of the sensors and assign them to pointers. The MUST be set to the last state true so they
//don't fail on the first change. See the initPins() function for details.
var lastState12p bool = true
var currState12p bool = false
var lastState13p bool = true
var currState13p bool = false
var lastState12 *bool = &lastState12p
var currState12 *bool = &currState12p
var lastState13 *bool = &lastState13p
var currState13 *bool = &currState13p

func main() {

	//Create a new Queue struct and set the parameters needed for it to run:
	//The sensors must be turned on seperately as they are not part of the Queue.
	q := NewQueue()
	q.MinMilli = 300
	q.MaxMilli = 2500

	//set up for the sensors:
	go initPins(q)
	go InfeedSensor(&q)
	go OutfeedSensor(&q)

	//Blinky is a simple blink program just to show that the board is alive and running.
	//There are no screen outputs attached to the board while running other than when debugging.
	go blinky()

	//Run a ticker to keep track of the memory heap. Every 5 seconds we check how much memory we are using.
	//The pcio rp2040 has 256kb available. Therefore, to be safe, we run a garbage collector at 100kb
	//so we never error and fail. Optimizations were doing to minimize this and more can still be done
	//but so far in actual practice right now the GC is running on average once every 10 minutes.
	stats := &runtime.MemStats{}
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	//turn off the error light via the NC switch. This will only get turned back on via the KillFunc()
	//if we encounter an error which will then end the program until a hardware reset is done via the RUN pin.
	//This way, the error light turns on whenever the board shuts down or fails
	//TODO: (if a panic happens this may or may not be the case and should be tested)
	pin10 := machine.GPIO10
	pin10.Configure(machine.PinConfig{Mode: machine.PinOutput})
	pin10.High()
	q.ErrorPin = pin10

	//set up queue receiver:
	//we simply loop to keep the program alive checking the memory and running the GC as needed.
	//Otherwise, if there is at any point a signal on the Kill channel, we run KillFunc() and shut down.
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

//Queue stuct has access to everything that is needed in order to manage the Queue of sheets.
//This includes the indications of the infeed and outfeed sensors turning on or off (which honestly could've
//been done with a single channel sending true or false rather than having two channels per sensor).
//It also has access to start sheet timers for ensuring a sheet doesn't get stuck in the tunnel and
//access to the KillFunc and errorPin so it can shut down the program. Most of the program, revolves around this struct.
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
	ErrorPin				machine.Pin
}

//Function t create the basic set ups for a Queue. This simply builds the struct but does not turn on
//all of the functions needed. These are done in main()
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
	}

	return p
}

//A wrapper for the TimerFunc but probably doesn't need to be done this way. This func originally did more...
//It is a bit neater to simply have a func to start and a func to stop the timer.
func (q Queue) StartTimer() {
	q.TimerFunc()
	return
}

//Send a signal to stop the timer which is running inside TimerFunc()
func (q Queue) StopTimer() {
	q.StopTimerChan <- true
}

// TimerFunc starts a timer and will send to q.Kill a true signal if ever reached.
// This is intended to be started once a sheet is in the UV tunnel and Q.Len is greater than 1.
//Whenever Q.Len is 0, there is no timer running as no sheets are in the UV tunnel.
func (q Queue) TimerFunc() {
	q.Timer = time.NewTimer(timerDuration * time.Second)
	defer q.Timer.Stop()
	for {
		select {
		//Timer fired which means a sheet got stuck. Kill the program.
		case <-q.Timer.C:
			if q.Q.Len() == 0 {
				println("Timer fired even though Q.Len() was 0. Should never happen!")
			}
			//q.Kill <- true
		//A sheet was removed from the tunnel, so reset the timer.
		case <-q.SheetRemovedChan:
			q.Timer.Reset(timerDuration * time.Second)
		//Stop the timer by use of a return and the defer will handle stopping
		case <-q.StopTimerChan:
			return
		}
	}
}

//KillFunc() is the single function to handle all errors. This turns on the error light on the control box
//shuts off the UV light, stops the feeding of the machine and then shuts down the program.
//Restarting is done on the control box via push button connected to the RUN pin on the pico.
func (q Queue) KillFunc() {

	//The errorPin is kept high with a NC circuit. Setting it to low, will turn on the error light on the
	//control box.
	q.ErrorPin.Low()

	//turn off the UV lights with a single 500ms pulse on pin11:
	//this will also stop the sheet feeding. Then shut down the program.
	//Use the RUN pin to reset the board which will start the program fresh.
	pin11 := machine.GPIO11
	pin11.Configure(machine.PinConfig{Mode: machine.PinOutput})
	pin11.High()
	time.Sleep(500 * time.Millisecond)
	pin11.Low()
	println("KillFunc() active. shutting down!")
	os.Exit(1)
	return

}

//This is the single goroutine which is monitoring the infeed sensor. This should
//never shut down. When the sensor turns on we start a ticker so we can get a pulse every
//second to check how long sheets have been under the sensor. If a sheet has been under a sensor too long,
//it means it's stuck and we need to error via the killFunc()
//When the Input is on, we are checking this every second until we get a signal on inpoutOff. Once it is off,
//we check to ensure the sensor was covered long enough to be an actual sheet. If so, we add the length of the
//sheet to the Queue so when it leaves the tunnel we can ensure the sheets are traveling smoothly. If there is
//too great a difference, it means there is sheet interruptions happens and we should shut down.
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
						println("Max number of sheets exceeded.", qLenMax)
						q.Kill <- true
					}
					println("Q-len: ", diff.Milliseconds(), q.Q.Len())
				} else {
					// println("saw something small on infeed")
				}
				continue wait
			case <-ticker.C:
				*diff = time.Since(*t)
				if diff.Milliseconds() > q.MaxMilli {
					println("Input sensor covered too long")
					q.Kill <- true
					return
				}
			}
		}
	}
}

//Similar to infeed sensor except it stops the timer if the Queue length is 0 (ie no sheets in UV tunnel)
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
						//this shouldn't happen but doesn't mean we should die because this would only
						//happen if our system is wrong and shouldn't shut down the machine for that as
						//as there is not threat
						println("nothing in Q but output got a sheet!")
						continue wait
					}
					//get the first element from the queue:
					el := q.Q.Front()
					if compare(el, diff.Milliseconds()) {
						// println("Got the right one")
						q.Q.Remove(el)
						println("Q Len= ", q.Q.Len())
						//if the Queue is empty we should stop the timer as nothing is in the tunnel.
						//else the Queue is not empty, we should reset the timer as we did just exit a sheet.
						//Therefore, nothing is stuck as sheets are exiting as expected
						if q.Q.Len() == 0 {
							q.StopTimer()
						} else {
							q.SheetRemovedChan <- true
						}
					} else {
						println("Sheet lengths don't match, something wrong!")
						q.Kill <- true
					}
				} else {
					// println("saw something small on outfeed")
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

//Set up the interrupts on pin12 and pin13. We use inerrupts as these must be live and very
//accurate as they are keeping track of timing. They take priority over everything else in the program.
func initPins(q Queue) {

	//12 & 13 set ups as inputPullup for sensors:
	pin12 := machine.GPIO12
	pin13 := machine.GPIO13
	pin12.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	pin13.Configure(machine.PinConfig{Mode: machine.PinInputPullup})

	//INFEED SENSOR
	pin12.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed: debounce to ensure noise doesn't activate.
		//find out if being pushed or released
		*currState12 = p.Get()
		//check if we are already pushed; if so, this is noise and ignore it
		if *lastState12 != *currState12 {
			//set our last state so we keep track of the last activity to check next time:
			*lastState12 = p.Get()
			//if false, the input sensor is on because this is a pullup. send a bool on the correct channel.
			if p.Get() == false {
				q.InputOn <- true
			} else {
				q.InputOff <- true
			}
		}
	})

	//OUTFEED SENSOR
	pin13.SetInterrupt(PinToggle, func(p machine.Pin) {
		//when pushed: see infeed sensor comments
		*currState13 = p.Get()
		if *lastState13 != *currState13 {
			*lastState13 = p.Get()
			if p.Get() == false {
				q.OutputOn <- true
			} else {
				q.OutputOff <- true
			}
		}
	})
}

//Compare simply compares the length of the sheet that was seen at the infeed sensor
//from what was seen at the outfeed sensor.
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


//Run a simple blinky program to show that the board is on, working and alive.
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
