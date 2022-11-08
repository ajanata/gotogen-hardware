//go:build matrixportal_m4

package main

import (
	"device/sam"
	"image/color"
	"machine"
	"time"

	"github.com/ajanata/gotogen"
	"github.com/ajanata/textbuf"
	"github.com/aykevl/things/hub75"
	"tinygo.org/x/drivers"
	"tinygo.org/x/drivers/ssd1306"
	"tinygo.org/x/drivers/ws2812"

	"github.com/ajanata/gotogen-hardware/internal/ntp"
)

const ntpHost = "time.nist.gov"

// we're using SERCOM4 for SPI on the built-in matrix connector, so we have to define it ourselves
var matrixSPI = machine.SPI{
	Bus:    sam.SERCOM4_SPIM,
	SERCOM: 4,
}

// pins for SERCOM4
const (
	matrixSCK = machine.PB09
	matrixSDO = machine.PB08
	matrixSDI = machine.NoPin
)

type driver struct {
	lastButton byte
}

func main() {
	time.Sleep(time.Second)
	blink()
	err := machine.I2C0.Configure(machine.I2CConfig{
		SCL:       machine.I2C0_SCL_PIN,
		SDA:       machine.I2C0_SDA_PIN,
		Frequency: 3.6 * machine.MHz,
	})
	if err != nil {
		earlyPanic(err)
	}
	blink()

	dev := ssd1306.NewI2C(machine.I2C0)
	dev.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	blink()
	dev.ClearBuffer()
	dev.ClearDisplay()
	blink()

	g, err := gotogen.New(60, nil, &dev, machine.LED, &driver{})
	if err != nil {
		earlyPanic(err)
	}
	err = g.Init()
	if err != nil {
		earlyPanic(err)
	}

	g.Run()
}

func (driver) EarlyInit() (faceDisplay drivers.Displayer, boopSensor gotogen.BoopSensor, err error) {
	// turn off the NeoPixel
	machine.NEOPIXEL.Configure(machine.PinConfig{Mode: machine.PinOutput})
	np := ws2812.New(machine.NEOPIXEL)
	_ = np.WriteColors([]color.RGBA{{}})

	err = matrixSPI.Configure(machine.SPIConfig{
		SDI:       matrixSDI,
		SDO:       matrixSDO,
		SCK:       matrixSCK,
		Frequency: 12 * machine.MHz,
	})
	if err != nil {
		return nil, nil, err
	}

	rgb := hub75.New(hub75.Config{
		Data:         matrixSDO,
		Clock:        matrixSCK,
		Latch:        machine.HUB75_LAT,
		OutputEnable: machine.HUB75_OE,
		A:            machine.PB00,
		B:            machine.PB02,
		C:            machine.PB03,
		D:            machine.PB05,
		Brightness:   0x1F,
		NumScreens:   4, // screens are 32x32 as far as this driver is concerned
	})

	// configure buttons
	machine.BUTTON_UP.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	machine.BUTTON_DOWN.Configure(machine.PinConfig{Mode: machine.PinInputPullup})

	return rgb, nil, nil
}

func (driver) LateInit(buf *textbuf.Buffer) error {
	err := ntp.NTP(ntpHost, wifiSSID, wifiPassword, tzOffset, buf)
	if err != nil {
		return err
	}
	return nil
}

func (d *driver) PressedButton() gotogen.MenuButton {
	// TODO figure out more buttons
	cur := byte(0)
	// buttons use pull-up resistors and short to ground, so they are *false* when pressed
	if !machine.BUTTON_UP.Get() {
		cur |= 1 << gotogen.MenuButtonUp
	}
	if !machine.BUTTON_DOWN.Get() {
		cur |= 1 << gotogen.MenuButtonDown
	}
	if cur == d.lastButton {
		// TODO key repeat
		return gotogen.MenuButtonNone
	}
	d.lastButton = cur
	// some button has changed
	if cur&(1<<gotogen.MenuButtonUp) > 0 {
		return gotogen.MenuButtonUp
	}
	if cur&(1<<gotogen.MenuButtonDown) > 0 {
		return gotogen.MenuButtonDown
	}

	// guess they let go of all the buttons
	return gotogen.MenuButtonNone
}
