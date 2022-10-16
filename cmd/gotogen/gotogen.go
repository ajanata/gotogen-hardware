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

func earlyPanic() {
	for {
		blink()
	}
}
