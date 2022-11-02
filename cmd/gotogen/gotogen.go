package main

import (
	"machine"
	"time"
)

var (
	// TODO better way to set these. for now, create a config.go and set them in an init()
	wifiSSID     string
	wifiPassword string
	tzOffset     time.Duration
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
