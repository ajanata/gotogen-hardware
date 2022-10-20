package main

import (
	"fmt"
	"machine"
	"time"

	"github.com/ajanata/textbuf"
	"tinygo.org/x/drivers/sdcard"
	"tinygo.org/x/drivers/ssd1306"
)

func blink() {
	led := machine.LED
	led.Configure(machine.PinConfig{Mode: machine.PinOutput})
	led.High()
	time.Sleep(100 * time.Millisecond)
	led.Low()
	time.Sleep(100 * time.Millisecond)
}

func main() {
	time.Sleep(time.Second)
	blink()
	machine.I2C0.Configure(machine.I2CConfig{
		SCL: machine.I2C0_SCL_PIN,
		SDA: machine.I2C0_SDA_PIN,
	})
	blink()

	dev := ssd1306.NewI2C(machine.I2C0)
	dev.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	blink()

	dev.ClearBuffer()
	dev.ClearDisplay()
	blink()

	buf, err := textbuf.New(&dev, textbuf.FontSize6x8)
	if err != nil {
		for {
			blink()
		}
	}

	buf.Println("playground boot")
	println("boot")

	machine.SPI9.Configure(machine.SPIConfig{
		// teensy41
		// Frequency: 0,
		// SDI: machine.SPI1_SDI_PIN,
		// SDO: machine.SPI1_SDO_PIN,
		// SCK: machine.SPI1_SCK_PIN,
		// CS:  machine.SPI1_SDI_PIN,
		// matrixportal-m4
		SDI: machine.SPI9_SDI_PIN,
		SDO: machine.SPI9_SDO_PIN,
		SCK: machine.SPI9_SCK_PIN,
	})
	println("spi config")
	// rgb := hub75.New(machine.SPI1, machine.D3, machine.D2)

	sd := sdcard.New(&machine.SPI9, machine.A1, machine.A2, machine.A3, machine.A4)
	println("sd new")
	err = sd.Configure()
	println("sd config")
	if err != nil {
		buf.PrintlnInverse("sd config: " + err.Error())
	} else {
		buf.Println(fmt.Sprintf("sd size: %d", sd.Size()))
	}

	// prox := apds9960.New(machine.I2C0)
	// prox.Configure(apds9960.Configuration{})
	// prox.EnableProximity()

	buf.PrintlnInverse("inverse")
	w, h := buf.Size()
	buf.Println(fmt.Sprintf("w, h = %d, %d", w, h))
	buf.SetLineInverse(5, "more inverse")

	for {
		time.Sleep(time.Second)
		blink()
		// buf.SetLine(7, fmt.Sprintf("prox: %d", prox.ReadProximity()))
	}
}
