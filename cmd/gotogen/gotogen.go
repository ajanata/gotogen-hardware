package main

import (
	"machine"
	"time"
)

func blink() {
	led := machine.LED
	led.Configure(machine.PinConfig{Mode: machine.PinOutput})
	led.High()
	time.Sleep(100 * time.Millisecond)
	led.Low()
	time.Sleep(100 * time.Millisecond)
}

func earlyPanic(err error) {
	for i := 0; ; i++ {
		blink()
		if i%5 == 0 {
			println(err)
		}
	}
}
