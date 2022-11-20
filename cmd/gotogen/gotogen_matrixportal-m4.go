//go:build matrixportal_m4

package main

import (
	"device/sam"
	"image/color"
	"machine"
	"runtime/interrupt"
	"time"
	"unsafe"

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

const dmaDescriptors = 2

//go:align 16
var DMADescriptorSection [dmaDescriptors]DMADescriptor

//go:align 16
var DMADescriptorWritebackSection [dmaDescriptors]DMADescriptor

type DMADescriptor struct {
	Btctrl   uint16
	Btcnt    uint16
	Srcaddr  unsafe.Pointer
	Dstaddr  unsafe.Pointer
	Descaddr unsafe.Pointer
}

type driver struct {
	lastButton byte
}

var d = driver{}
var disp *ssd1306.Device

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

	// Init DMAC.
	// First configure the clocks, then configure the DMA descriptors. Those
	// descriptors must live in SRAM and must be aligned on a 16-byte boundary.
	// http://www.lucadavidian.com/2018/03/08/wifi-controlled-neo-pixels-strips/
	// https://svn.larosterna.com/oss/trunk/arduino/zerotimer/zerodma.cpp
	sam.MCLK.AHBMASK.SetBits(sam.MCLK_AHBMASK_DMAC_)
	sam.DMAC.BASEADDR.Set(uint32(uintptr(unsafe.Pointer(&DMADescriptorSection))))
	sam.DMAC.WRBADDR.Set(uint32(uintptr(unsafe.Pointer(&DMADescriptorWritebackSection))))
	// Enable peripheral with all priorities.
	sam.DMAC.CTRL.SetBits(sam.DMAC_CTRL_DMAENABLE | sam.DMAC_CTRL_LVLEN0 | sam.DMAC_CTRL_LVLEN1 | sam.DMAC_CTRL_LVLEN2 | sam.DMAC_CTRL_LVLEN3)

	disp = ssd1306.NewI2CDMA(machine.I2C0, &ssd1306.DMAConfig{
		DMADescriptor: (*ssd1306.DMADescriptor)(&DMADescriptorSection[1]),
		DMAChannel:    1,
		TriggerSource: 0x0F, // SERCOM5_DMAC_ID_TX
	})
	disp.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	i2cInt := interrupt.New(sam.IRQ_DMAC_1, dispDMAInt)
	i2cInt.SetPriority(0xC0)
	i2cInt.Enable()
	blink()
	disp.ClearDisplay()
	blink()

	g, err := gotogen.New(60, nil, disp, machine.LED, &d)
	if err != nil {
		earlyPanic(err)
	}
	err = g.Init()
	if err != nil {
		earlyPanic(err)
	}

	g.Run()
}

func dispDMAInt(i interrupt.Interrupt) {
	disp.TXComplete(i)
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
		DeviceConfig: hub75.DeviceConfig{
			Bus:                   &matrixSPI,
			TriggerSource:         0x0D, // SERCOM4_DMAC_ID_TX
			OETimerCounterControl: sam.TCC3,
			TimerChannel:          0,
			TimerIntenset:         sam.TCC_INTENSET_MC0,
			DMAChannel:            0,
			DMADescriptor:         (*hub75.DmaDescriptor)(&DMADescriptorSection[0]),
		},
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
	spiInt := interrupt.New(sam.IRQ_SERCOM4_1, hub75.SPIHandler)
	spiInt.SetPriority(0xC0)
	spiInt.Enable()
	timerInt := interrupt.New(sam.IRQ_TCC3_MC0, hub75.TimerHandler)
	timerInt.SetPriority(0xC0)
	timerInt.Enable()

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
