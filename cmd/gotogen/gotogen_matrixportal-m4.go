//go:build matrixportal_m4

package main

import (
	"device/sam"
	"image/color"
	"machine"
	"runtime"
	"runtime/interrupt"
	"strconv"
	"time"
	"unsafe"

	"github.com/ajanata/gotogen"
	"github.com/ajanata/textbuf"
	"github.com/aykevl/things/hub75"
	"tinygo.org/x/drivers/apds9960"
	"tinygo.org/x/drivers/flash"
	"tinygo.org/x/drivers/lis3dh"
	"tinygo.org/x/drivers/mpr121"
	"tinygo.org/x/drivers/pcf8523"
	"tinygo.org/x/drivers/pcf8574"
	"tinygo.org/x/drivers/ssd1306"
	"tinygo.org/x/drivers/ws2812"
	"tinygo.org/x/tinyfs"
	"tinygo.org/x/tinyfs/littlefs"

	"github.com/ajanata/gotogen-hardware/internal/mic"
	"github.com/ajanata/gotogen-hardware/internal/ntp"
)

// const pcf8574Address = 0x20 // adafruit breakout
const pcf8574Address = pcf8574.DefaultAddress // bare chip

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

// and using SERCOM0 for SPI for the OLED display
var oledSPI = machine.SPI{
	Bus:    sam.SERCOM0_SPIM,
	SERCOM: 0,
}

// pins for SERCOM0
const (
	oledMOSI = machine.PA04
	oledSCK  = machine.PA05
	oledCS   = machine.PB01
	oledDC   = machine.PB15
	oledRST  = machine.PB04
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

	np ws2812.Device

	lastButton   byte
	menuDisp     *dispWrapper
	faceDisp     *rgbWrapper
	rtc          pcf8523.Device
	prox         *apds9960.Device
	accel        *lis3dh.Device
	fl           *flash.Device
	fs           tinyfs.Filesystem
	gpio         *pcf8574.Device
	touch        *mpr121.Device
	touchEnabled bool

	mic        *mic.Mic
	talkCutoff float32
}

var d = driver{
	np: ws2812.New(machine.NEOPIXEL),

	// TODO load from settings
	talkCutoff: 3000,
}

type dispWrapper struct {
	*ssd1306.Device
}

type rgbWrapper struct {
	*hub75.Device
}

func main() {
	// enable the cache controller to massively increase execution speed
	sam.CMCC.CTRL.SetBits(sam.CMCC_CTRL_CEN)

	// turn on the NeoPixel to indicate boot
	machine.NEOPIXEL.Configure(machine.PinConfig{Mode: machine.PinOutput})
	_ = d.np.WriteColors([]color.RGBA{{R: 0x30, B: 0x30}})

	time.Local = time.FixedZone("local", int(tzOffset.Seconds()))
	time.Sleep(time.Second)
	err := machine.I2C0.Configure(machine.I2CConfig{
		SCL:       machine.I2C0_SCL_PIN,
		SDA:       machine.I2C0_SDA_PIN,
		Frequency: 400 * machine.KHz,
	})
	if err != nil {
		earlyPanic(err)
	}
	println("starting early boot")

	err = oledSPI.Configure(machine.SPIConfig{
		SCK:       oledSCK,
		SDO:       oledMOSI,
		SDI:       machine.NoPin,
		Frequency: 20 * machine.MHz,
	})
	if err != nil {
		earlyPanic(err)
	}

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

	// non-DMA SPI
	// disp := ssd1306.NewSPI(oledSPI, oledDC, oledRST, oledCS)
	// DMA SPI
	disp := ssd1306.NewSPIDMA(&oledSPI, oledDC, oledRST, oledCS, &ssd1306.DMAConfig{
		DMADescriptor: (*ssd1306.DMADescriptor)(&DMADescriptorSection[1]),
		DMAChannel:    1,
		TriggerSource: 0x05, // SERCOM0_DMAC_ID_TX
	})

	d.menuDisp = &dispWrapper{Device: disp}
	d.menuDisp.Configure(ssd1306.Config{
		Width:    128,
		Height:   64,
		VccState: ssd1306.SWITCHCAPVCC,
	})
	oledInt := interrupt.New(sam.IRQ_SERCOM0_1, oledDMAInt)
	oledInt.SetPriority(0xC0)
	oledInt.Enable()

	d.menuDisp.ClearDisplay()

	println("starting gotogen boot")

	g, err := gotogen.New(120, d.menuDisp, machine.LED, &d)
	if err != nil {
		earlyPanic(err)
	}

	d.g = g
	err = g.Init()
	if err != nil {
		earlyPanic(err)
	}

	d.g.Run()
}

func oledDMAInt(i interrupt.Interrupt) {
	d.menuDisp.SPITXComplete(i)
}

func (w *dispWrapper) CanUpdateNow() bool {
	return !w.Busy()
}

func (*rgbWrapper) CanUpdateNow() bool { return true }

func (d *driver) waitForDMA() {
	// ensure no active I2C DMA transfers for the display
	for d.menuDisp.Busy() {
		time.Sleep(time.Millisecond)
	}
}

func (d *driver) EarlyInit() (faceDisplay gotogen.Display, err error) {
	err = matrixSPI.Configure(machine.SPIConfig{
		SDI:       matrixSDI,
		SDO:       matrixSDO,
		SCK:       matrixSCK,
		Frequency: 12 * machine.MHz,
	})
	if err != nil {
		println("spi config", err)
		return nil, err
	}

	d.faceDisp = &rgbWrapper{Device: hub75.New(hub75.Config{
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
		Latch:        machine.PB06,
		OutputEnable: machine.HUB75_OE,
		A:            machine.PB00,
		B:            machine.PB02,
		C:            machine.PB03,
		D:            machine.PB05,
		Brightness:   0x20,
		NumScreens:   4, // screens are 32x32 as far as this driver is concerned
	})}
	spiInt := interrupt.New(sam.IRQ_SERCOM4_1, hub75.SPIHandler)
	spiInt.SetPriority(0xC0)
	spiInt.Enable()
	timerInt := interrupt.New(sam.IRQ_TCC3_MC0, hub75.TimerHandler)
	timerInt.SetPriority(0xC0)
	timerInt.Enable()

	// configure buttons
	machine.BUTTON_UP.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	machine.BUTTON_DOWN.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	// TODO configure "interrupt" for pcf8574

	return d.faceDisp, nil
}

func (d *driver) LateInit(buf *textbuf.Buffer) {
	_ = buf.Print("Reading RTC")
	d.waitForDMA()
	d.rtc = pcf8523.New(machine.I2C0)
	rtcGood := false
	lost, err := d.rtc.LostPower()
	if err != nil {
		println("rtc lost power check:", err)
		_ = buf.PrintlnInverse(": " + err.Error())
		_ = buf.Println("Skipping RTC")
	} else {
		init, err := d.rtc.Initialized()
		if err != nil {
			println("rtc initialized check:", err)
			_ = buf.PrintlnInverse(": " + err.Error())
			_ = buf.Println("Skipping RTC")
		} else {
			if init && !lost {
				now, err := d.rtc.Now()
				if err != nil {
					println("rtc read:", err)
					_ = buf.PrintlnInverse(": " + err.Error())
					_ = buf.Println("Skipping RTC")
				} else {
					runtime.AdjustTimeOffset(-1 * int64(time.Since(now)))
					if now.Year() > 2050 || now.Year() < 2022 {
						_ = buf.PrintlnInverse(": bogus")
						println("rtc bogus: ", now.String())
					} else {
						_ = buf.Println(".")
						println("using rtc")
						rtcGood = true
					}
				}
			} else {
				println("not using rtc")
				_ = buf.PrintlnInverse(": RTC lost power or not set, ignoring")
			}
		}
	}

	if !rtcGood {
		// TODO check a button for bypass (e.g. if known that wifi network isn't in range)
		err := ntp.NTP(ntpHost, wifiSSID, wifiPassword, buf)
		if err != nil {
			println("ntp:", err)
		} else {
			println(time.Now().String())
			d.waitForDMA()
			err := d.rtc.Set(time.Now().In(time.UTC))
			if err != nil {
				println("setting rtc:", err)
			}
		}
	}

	// boop sensor isn't working through the visor :(
	// _ = buf.Print("Proximity")
	// d.waitForDMA()
	// prox := apds9960.New(machine.I2C0)
	// d.prox = &prox
	// d.prox.Configure(apds9960.Configuration{
	// 	LEDBoost: 300,
	// 	// ProximityGain:        8,
	// 	// ProximityPulseCount:  64,
	// 	// ProximityPulseLength: 32,
	// })
	// // the example I have returns 0xA8 for the device ID, not 0xAB, so the driver thinks it isn't there.
	// // but it does actually work, so I guess just always assume it's there
	// // if d.prox.Connected() && d.prox.ProximityAvailable() {
	// d.prox.EnableProximity()
	// _ = buf.Println(".")
	// // } else {
	// // 	_ = buf.PrintlnInverse(": unavailable")
	// // 	d.prox = nil
	// // }

	_ = buf.Print("Accelerometer")
	d.waitForDMA()
	accel := lis3dh.New(machine.I2C0)
	d.accel = &accel
	d.accel.Address = 0x19
	d.accel.Configure()
	if d.accel.Connected() {
		// hopefully this saves power?
		d.accel.SetDataRate(lis3dh.DATARATE_50_HZ)
		_ = buf.Println(".")
	} else {
		println("accelerometer:", err)
		_ = buf.PrintlnInverse(": unavailable")
		d.accel = nil
	}

	_ = buf.Print("Mic")
	d.mic = mic.New(machine.PA07, 100)

	_ = buf.Print(".\nGPIO")
	d.gpio = pcf8574.New(machine.I2C0)
	d.gpio.Configure(pcf8574.Config{
		Address: pcf8574Address,
	})

	_ = buf.Print(".\nCapacitive Touch")
	d.touch = mpr121.New(machine.I2C0)
	err = d.touch.Configure(mpr121.Config{
		Address:          mpr121.DefaultAddress,
		TouchThreshold:   0x10,
		ReleaseThreshold: 0x05,
		ProximityMode:    0,
		AutoConfig:       true,
	})
	if err != nil {
		println("capacitive touch: " + err.Error())
		_ = buf.PrintlnInverse(": " + err.Error())
		d.touch = nil
	} else {
		_ = buf.Println(".")
	}

	_ = buf.Print("Flash")
	f := flash.NewQSPI(machine.D42, machine.D41, machine.D43, machine.D44, machine.D45, machine.D46)
	// TODO we know we're only going to have a GD25Q16 so make a device identifier specifically for that for code size
	err = f.Configure(&flash.DeviceConfig{Identifier: flash.DefaultDeviceIdentifier})
	if err != nil {
		println("flash:", err)
		_ = buf.PrintlnInverse(": " + err.Error())
	} else {
		d.fl = f
		s := f.Size()
		_ = buf.Println(": " + strconv.FormatInt(s>>10, 10) + "KiB")
		_ = buf.Print("Filesystem")
		fs := littlefs.New(f)
		// copied these values from the example, may need tuning
		fs.Configure(&littlefs.Config{
			CacheSize:     512,
			LookaheadSize: 512,
			BlockCycles:   100,
		})
		err := fs.Mount()
		if err != nil {
			println("mount fs:", err)
			_ = buf.PrintlnInverse(": " + err.Error())
		} else {
			s, err := fs.Size()
			if err != nil {
				println("getting fs size:", err)
				_ = buf.PrintlnInverse(": " + err.Error())
			} else {
				d.fs = fs
				_ = buf.Println(": " + strconv.Itoa(s))
			}
		}
	}

	// turn off the NeoPixel
	_ = d.np.WriteColors([]color.RGBA{{}})
}

const (
	ioBack = iota
	ioMenu
	ioUp
	ioDown
	ioExtra1
	ioExtra2
	ioToggleTouch
	ioTouchEvent
)

const (
	touchMenu = iota
	touchBack
	touchDown
	touchUp
	touchExtra1
	touchExtra2
)

func (d *driver) PressedButton() gotogen.MenuButton {
	cur := byte(0)
	// buttons use pull-up resistors and short to ground, so they are *false* when pressed
	if !machine.BUTTON_UP.Get() {
		cur |= 1 << gotogen.MenuButtonUp
	}
	if !machine.BUTTON_DOWN.Get() {
		cur |= 1 << gotogen.MenuButtonDown
	}

	// TODO check the "interrupt" input from the PCF8574 before asking for its values
	r, err := d.gpio.Read()
	if err != nil {
		println("reading GPIO expander: " + err.Error())
	} else {
		if !r.Pin(ioBack) {
			cur |= 1 << ioBack
		}
		if !r.Pin(ioMenu) {
			cur |= 1 << ioMenu
		}
		if !r.Pin(ioUp) {
			cur |= 1 << ioUp
		}
		if !r.Pin(ioDown) {
			cur |= 1 << ioDown
		}
		if !r.Pin(ioToggleTouch) {
			cur |= 1 << ioToggleTouch
		}

		// capacitive touch "interrupt"
		if !r.Pin(ioTouchEvent) && d.touch != nil {
			tr, err := d.touch.Status()
			if err != nil {
				println("reading capacitive touch: " + err.Error())
			} else if d.touchEnabled {
				if tr.Touched(touchMenu) || tr.Touched(touchExtra1) || tr.Touched(touchExtra2) {
					cur |= 1 << ioMenu
				}
				if tr.Touched(touchBack) {
					cur |= 1 << ioBack
				}
				if tr.Touched(touchDown) {
					cur |= 1 << ioDown
				}
				if tr.Touched(touchUp) {
					cur |= 1 << ioUp
				}
			}
		}
	}

	if cur == d.lastButton {
		// TODO key repeat
		return gotogen.MenuButtonNone
	}

	d.lastButton = cur
	// some button has changed
	if cur&(1<<ioUp) > 0 {
		return gotogen.MenuButtonUp
	}
	if cur&(1<<ioDown) > 0 {
		return gotogen.MenuButtonDown
	}
	if cur&(1<<ioBack) > 0 {
		return gotogen.MenuButtonBack
	}
	if cur&(1<<ioMenu) > 0 {
		return gotogen.MenuButtonMenu
	}
	if cur&(1<<ioToggleTouch) > 0 {
		d.touchEnabled = !d.touchEnabled
	}

	// guess they let go of all the buttons
	return gotogen.MenuButtonNone
}

func (d *driver) BoopDistance() (uint8, gotogen.SensorStatus) {
	return 0, gotogen.SensorStatusUnavailable
	// if d.prox == nil {
	// 	return 0, gotogen.SensorStatusUnavailable
	// }
	// if d.menuDisp.Busy() {
	// 	return 0, gotogen.SensorStatusBusy
	// }
	// // TODO normalize
	// return uint8(d.prox.ReadProximity()), gotogen.SensorStatusAvailable
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
	return x / 1000, y / 1000, z / 1000, gotogen.SensorStatusAvailable
}

func (d *driver) Talking() bool {
	return d.mic.Value() > d.talkCutoff
}

func (d *driver) MenuItems() []gotogen.Item {
	formatLabel := "Yes (not formatted)"
	if d.fs != nil {
		formatLabel = "Yes (WILL ERASE)"
	}

	m := []gotogen.Item{
		&gotogen.SettingItem{
			Name:    "Brightness",
			Options: []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"},
			Active:  uint8(d.faceDisp.Brightness() >> 3),
			Default: 4,
			Apply:   d.setBrightness,
		},
		&gotogen.ActionItem{
			Name:   "Set time from NTP",
			Invoke: d.setTime,
		},
		&gotogen.SettingItem{
			Name:    "Talking cutoff",
			Options: []string{"2000", "2500", "3000", "3500", "4000", "4500", "5000"},
			Active:  uint8((d.talkCutoff - 2000) / 500),
			Apply:   d.setTalkCutoff,
		},
		&gotogen.Menu{
			Name: "Format flash",
			Items: []gotogen.Item{
				&gotogen.ActionItem{
					Name:   "No",
					Invoke: func() {},
				},
				&gotogen.ActionItem{
					Name:   formatLabel,
					Invoke: d.formatFlash,
				},
			},
		},
	}

	return m
}

func (d *driver) StatusLine() string {
	if d.touchEnabled {
		return "Touch: on"
	}
	return "Touch: off"
}

func (d *driver) setTime() {
	d.g.Busy(func(buf *textbuf.Buffer) {
		buf.AutoFlush = true
		err := ntp.NTP(ntpHost, wifiSSID, wifiPassword, buf)
		if err != nil {
			_ = buf.PrintlnInverse("ntp: " + err.Error())
		} else {
			_ = buf.Println(time.Now().Format(time.Stamp))
			_ = buf.Print("Setting RTC")
			d.waitForDMA()
			err := d.rtc.Set(time.Now().In(time.UTC))
			if err != nil {
				_ = buf.PrintlnInverse("rtc: " + err.Error())
			}
			_ = buf.Println(".")
		}
	})
}

func (d *driver) setTalkCutoff(s uint8) {
	d.talkCutoff = 2000 + 500*float32(s)
}

func (d *driver) setBrightness(s uint8) {
	d.faceDisp.SetBrightness(uint32(s) << 3)
}

func (d *driver) formatFlash() {
	d.g.Busy(func(buf *textbuf.Buffer) {
		if d.fl == nil {
			_ = buf.PrintlnInverse("Flash chip failed initialization, reboot to try again.")
			return
		}
		if d.fs != nil {
			_ = buf.PrintlnInverse("ALREADY MOUNTED: will erase existing data. You have 5 seconds to abort.")
			time.Sleep(5 * time.Second)
			_ = buf.Print("Unmounting")
			err := d.fs.Unmount()
			if err != nil {
				_ = buf.PrintlnInverse(": " + err.Error())
				// try to continue anyway
			} else {
				_ = buf.Println(".")
			}
			d.fs = nil
		}
		_ = buf.Print("Erasing flash")
		err := d.fl.EraseAll()
		if err != nil {
			_ = buf.PrintlnInverse(": " + err.Error())
			return
		}

		_ = buf.Print(".\nFormatting")
		fs := littlefs.New(d.fl)
		// copied these values from the example, may need tuning
		fs.Configure(&littlefs.Config{
			CacheSize:     512,
			LookaheadSize: 512,
			BlockCycles:   100,
		})
		err = fs.Format()
		if err != nil {
			_ = buf.PrintlnInverse(": " + err.Error())
			return
		}

		_ = buf.Print(".\nMounting")
		err = fs.Mount()
		if err != nil {
			_ = buf.PrintlnInverse(": " + err.Error())
			return
		}

		_ = buf.Print(".\nSize: ")
		size, err := fs.Size()
		if err != nil {
			_ = buf.PrintlnInverse(err.Error())
			return
		}
		_ = buf.Println(strconv.Itoa(size))
		d.fs = fs
	})
}
