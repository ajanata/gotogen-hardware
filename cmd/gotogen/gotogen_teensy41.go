//go:build teensy41

package main

import (
	"machine"

	"github.com/ajanata/gotogen"
	"tinygo.org/x/drivers"
	"tinygo.org/x/drivers/hub75"
	"tinygo.org/x/drivers/ssd1306"
)

func main() {
	blink()
	machine.I2C0.Configure(machine.I2CConfig{
		SCL:       machine.I2C1_SCL_PIN,
		SDA:       machine.I2C1_SDA_PIN,
		Frequency: 2 * machine.MHz,
	})
	blink()

	dev := ssd1306.NewI2C(machine.I2C0)
	dev.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	blink()
	dev.ClearBuffer()
	dev.ClearDisplay()
	blink()

	// NOTE: we cannot blink the LED after we init SPI1 since its SCK is the same pin as the LED, so we actually can't
	// use machine.LED for the Blinker.
	// TODO try using a different SPI.
	g, err := gotogen.New(60, nil, &dev, nil, func() (faceDisplay drivers.Displayer, menuInput gotogen.MenuInput, boopSensor gotogen.BoopSensor, err error) {
		machine.SPI1.Configure(machine.SPIConfig{
			// Frequency: 25 * machine.MHz,
			Frequency: 18 * machine.MHz,
			SDI:       machine.SPI1_SDI_PIN,
			SDO:       machine.SPI1_SDO_PIN,
			SCK:       machine.SPI1_SCK_PIN,
			CS:        machine.SPI1_CS_PIN,
		})
		rgb := hub75.New(machine.SPI1, machine.D3, machine.D2, machine.D6, machine.D7, machine.D8, machine.D9)
		rgb.Configure(hub75.Config{
			Width:      128,
			Height:     32,
			ColorDepth: 3,
			RowPattern: 16,
			FastUpdate: true,
			// Brightness: 0x3F,
		})
		rgb.ClearDisplay()

		return &rgb, nil, nil, nil
	})
	if err != nil {
		earlyPanic(err)
	}
	err = g.Init()
	if err != nil {
		earlyPanic(err)
	}

	g.Run()
}
