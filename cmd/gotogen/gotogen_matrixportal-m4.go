//go:build matrixportal_m4

package main

import (
	"device/sam"
	"image/color"
	"machine"
	"runtime"
	"runtime/interrupt"
	"time"
	"tinygo.org/x/drivers/apds9960"
	"tinygo.org/x/drivers/lis3dh"
	"tinygo.org/x/drivers/pcf8523"
	"unsafe"

	"github.com/ajanata/gotogen"
	"github.com/ajanata/textbuf"
	"github.com/aykevl/things/hub75"
	"tinygo.org/x/drivers"
	"tinygo.org/x/drivers/ssd1306"
	"tinygo.org/x/drivers/ws2812"

	"github.com/ajanata/gotogen-hardware/internal/ntp"
)

const (
	up   = machine.BUTTON_UP
	down = machine.BUTTON_DOWN
	back = machine.A1
	menu = machine.A2
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
	g *gotogen.Gotogen

	lastButton byte
	menuDisp   *ssd1306.Device
	faceDisp   *hub75.Device
	rtc        pcf8523.Device
	prox       *apds9960.Device
	accel      *lis3dh.Device
}

var d = driver{}

func main() {
	time.Sleep(time.Second)
	blink()
	err := machine.I2C0.Configure(machine.I2CConfig{
		SCL:       machine.I2C0_SCL_PIN,
		SDA:       machine.I2C0_SDA_PIN,
		Frequency: machine.MHz,
		// display can go this fast, but the RTC can't...
		// Frequency: 3.6 * machine.MHz,
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

	d.menuDisp = ssd1306.NewI2CDMA(machine.I2C0, &ssd1306.DMAConfig{
		DMADescriptor: (*ssd1306.DMADescriptor)(&DMADescriptorSection[1]),
		DMAChannel:    1,
		TriggerSource: 0x0F, // SERCOM5_DMAC_ID_TX
	})
	d.menuDisp.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	i2cInt := interrupt.New(sam.IRQ_DMAC_1, dispDMAInt)
	i2cInt.SetPriority(0xC0)
	i2cInt.Enable()
	blink()
	d.menuDisp.ClearDisplay()
	blink()

	g, err := gotogen.New(60, d.menuDisp, machine.LED, &d)
	if err != nil {
		earlyPanic(err)
	}
	err = g.Init()
	if err != nil {
		earlyPanic(err)
	}

	d.g = g
	d.g.Run()
}

func dispDMAInt(i interrupt.Interrupt) {
	d.menuDisp.TXComplete(i)
}

func (*driver) EarlyInit() (faceDisplay drivers.Displayer, err error) {
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
		return nil, err
	}

	d.faceDisp = hub75.New(hub75.Config{
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
		Brightness:   0x20,
		NumScreens:   4, // screens are 32x32 as far as this driver is concerned
	})
	spiInt := interrupt.New(sam.IRQ_SERCOM4_1, hub75.SPIHandler)
	spiInt.SetPriority(0xC0)
	spiInt.Enable()
	timerInt := interrupt.New(sam.IRQ_TCC3_MC0, hub75.TimerHandler)
	timerInt.SetPriority(0xC0)
	timerInt.Enable()

	// configure buttons
	up.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	down.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	back.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	menu.Configure(machine.PinConfig{Mode: machine.PinInputPullup})

	return d.faceDisp, nil
}

func (*driver) LateInit(buf *textbuf.Buffer) error {
	_ = buf.Print("Reading RTC")
	// ensure no active I2C DMA transfers for the display
	for d.menuDisp.Busy() {
		time.Sleep(time.Millisecond)
	}
	d.rtc = pcf8523.New(machine.I2C0)
	rtcGood := false
	lost, err := d.rtc.LostPower()
	if err != nil {
		_ = buf.PrintlnInverse(": " + err.Error())
		_ = buf.Println("Skipping RTC")
	} else {
		init, err := d.rtc.Initialized()
		if err != nil {
			_ = buf.PrintlnInverse(": " + err.Error())
			_ = buf.Println("Skipping RTC")
		} else {
			if init && !lost {
				now, err := d.rtc.Now()
				if err != nil {
					_ = buf.PrintlnInverse(": " + err.Error())
					_ = buf.Println("Skipping RTC")
				} else {
					rtcGood = true
					runtime.AdjustTimeOffset(-1*int64(time.Since(now)) + int64(tzOffset))
					_ = buf.Println(".")
				}
			} else {
				_ = buf.PrintlnInverse(": RTC lost power or not set, ignoring")
			}
		}
	}

	if !rtcGood {
		err := ntp.NTP(ntpHost, wifiSSID, wifiPassword, tzOffset, buf)
		if err != nil {
			return err
		}
	}

	_ = buf.Print("Proximity")
	// ensure no active I2C DMA transfers for the display
	for d.menuDisp.Busy() {
		time.Sleep(time.Millisecond)
	}
	prox := apds9960.New(machine.I2C0)
	d.prox = &prox
	d.prox.Configure(apds9960.Configuration{})
	// the example I have returns 0xA8 for the device ID, not 0xAB, so the driver thinks it isn't there.
	// but it does actually work, so I guess just always assume it's there
	// if d.prox.Connected() && d.prox.ProximityAvailable() {
	d.prox.EnableProximity()
	_ = buf.Println(".")
	// } else {
	// 	_ = buf.PrintlnInverse(": unavailable")
	// 	d.prox = nil
	// }

	_ = buf.Print("Accelerometer")
	// ensure no active I2C DMA transfers for the display
	for d.menuDisp.Busy() {
		time.Sleep(time.Millisecond)
	}
	accel := lis3dh.New(machine.I2C0)
	d.accel = &accel
	d.accel.Address = 0x19
	d.accel.Configure()
	if d.accel.Connected() {
		// hopefully this saves power?
		d.accel.SetDataRate(lis3dh.DATARATE_50_HZ)
		_ = buf.Println(".")
	} else {
		_ = buf.PrintlnInverse(": unavailable")
		d.accel = nil
	}

	return nil
}

func (d *driver) PressedButton() gotogen.MenuButton {
	cur := byte(0)
	// buttons use pull-up resistors and short to ground, so they are *false* when pressed
	if !up.Get() {
		cur |= 1 << gotogen.MenuButtonUp
	}
	if !down.Get() {
		cur |= 1 << gotogen.MenuButtonDown
	}
	if !back.Get() {
		cur |= 1 << gotogen.MenuButtonBack
	}
	if !menu.Get() {
		cur |= 1 << gotogen.MenuButtonMenu
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
	if cur&(1<<gotogen.MenuButtonBack) > 0 {
		return gotogen.MenuButtonBack
	}
	if cur&(1<<gotogen.MenuButtonMenu) > 0 {
		return gotogen.MenuButtonMenu
	}

	// guess they let go of all the buttons
	return gotogen.MenuButtonNone
}

func (d *driver) BoopDistance() (uint8, gotogen.SensorStatus) {
	if d.prox == nil {
		return 0, gotogen.SensorStatusUnavailable
	}
	if d.menuDisp.Busy() {
		return 0, gotogen.SensorStatusBusy
	}
	// TODO normalize
	return uint8(d.prox.ReadProximity()), gotogen.SensorStatusAvailable
}

func (d *driver) Accelerometer() (int32, int32, int32, gotogen.SensorStatus) {
	if d.accel == nil {
		return 0, 0, 0, gotogen.SensorStatusUnavailable
	}
	if d.menuDisp.Busy() {
		return 0, 0, 0, gotogen.SensorStatusBusy
	}
	// TODO normalize and zero out gravity
	// this never returns an error...
	x, y, z, _ := d.accel.ReadAcceleration()
	return x, y, z, gotogen.SensorStatusAvailable
}

func (d *driver) MenuItems() []gotogen.Item {
	m := []gotogen.Item{
		&gotogen.ActionItem{
			Name:   "Set time from NTP",
			Invoke: d.setTime,
		},
		&gotogen.SettingItem{
			Name:    "Brightness",
			Options: []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"},
			Active:  uint8(d.faceDisp.Brightness() >> 3),
			Default: 4,
			Apply:   d.setBrightness,
		},
	}
	return m
}

func (d *driver) setTime() {
	d.g.Busy(func(buf *textbuf.Buffer) {
		buf.AutoFlush = true
		err := ntp.NTP(ntpHost, wifiSSID, wifiPassword, tzOffset, buf)
		if err != nil {
			_ = buf.PrintlnInverse("ntp: " + err.Error())
		} else {
			_ = buf.Println(time.Now().Format(time.Stamp))
			_ = buf.Print("Setting RTC")
			err := d.rtc.Set(time.Now())
			if err != nil {
				_ = buf.PrintlnInverse("rtc: " + err.Error())
			}
			_ = buf.Println(".")
		}
	})
}

func (d *driver) setBrightness(s uint8) {
	d.faceDisp.SetBrightness(uint32(s) << 3)
}
