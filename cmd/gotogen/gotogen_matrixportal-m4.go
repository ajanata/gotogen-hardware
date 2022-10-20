//go:build matrixportal_m4

package main

import (
	"machine"
	"time"

	"github.com/ajanata/gotogen"
	"github.com/aykevl/things/hub75"
	"tinygo.org/x/drivers"
	"tinygo.org/x/drivers/ssd1306"
)

func main() {
	time.Sleep(time.Second)
	blink()
	machine.I2C0.Configure(machine.I2CConfig{
		SCL:       machine.I2C0_SCL_PIN,
		SDA:       machine.I2C0_SDA_PIN,
		Frequency: 2 * machine.MHz,
	})
	blink()

	dev := ssd1306.NewI2C(machine.I2C0)
	dev.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	blink()
	dev.ClearBuffer()
	dev.ClearDisplay()
	blink()

	g, err := gotogen.New(60, nil, &dev, machine.LED, func() (faceDisplay drivers.Displayer, menuInput gotogen.MenuInput, boopSensor gotogen.BoopSensor, err error) {
		// matrixportal hub75 connector
		// rgb := rgb75.New(
		// 	machine.HUB75_OE, machine.HUB75_LAT, machine.HUB75_CLK,
		// 	[6]machine.Pin{
		// 		machine.HUB75_R1, machine.HUB75_G1, machine.HUB75_B1,
		// 		machine.HUB75_R2, machine.HUB75_G2, machine.HUB75_B2,
		// 	},
		// 	[]machine.Pin{
		// 		machine.HUB75_ADDR_A, machine.HUB75_ADDR_B, machine.HUB75_ADDR_C,
		// 		machine.HUB75_ADDR_D, machine.HUB75_ADDR_E,
		// 	})
		// err = rgb.Configure(rgb75.Config{
		// 	Width:      64,
		// 	Height:     32,
		// 	ColorDepth: 4,
		// 	DoubleBuf:  true,
		// })
		// if err != nil {
		// 	return nil, nil, nil, errors.New("face init: " + err.Error())
		// }
		// rgb.ClearDisplay()
		// rgb.Resume()

		// hacky spi. this currently uses a locally hacked tinygo machine definition. I'll clean it up soon.
		err = machine.SPI9.Configure(machine.SPIConfig{
			SDI:       machine.SPI9_SDI_PIN,
			SDO:       machine.SPI9_SDO_PIN,
			SCK:       machine.SPI9_SCK_PIN,
			Frequency: 12 * machine.MHz,
		})
		if err != nil {
			return nil, nil, nil, err
		}

		rgb := hub75.New(hub75.Config{
			Data:         machine.SPI9_SDO_PIN,
			Clock:        machine.SPI9_SCK_PIN,
			Latch:        machine.HUB75_LAT,
			OutputEnable: machine.HUB75_OE,
			A:            machine.HUB75_ADDR_A,
			B:            machine.HUB75_ADDR_B,
			C:            machine.HUB75_ADDR_C,
			D:            machine.HUB75_ADDR_D,
			Brightness:   0x1F,
			NumScreens:   4, // screens are 32x32 as far as this driver is concerned
		})

		return rgb, nil, nil, nil
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
