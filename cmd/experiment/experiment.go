package main

import (
	"device/sam"
	"fmt"
	"image/color"
	"machine"
	"math"
	"runtime"
	"runtime/interrupt"
	"strconv"
	"time"
	"unsafe"

	"github.com/ajanata/textbuf"
	"tinygo.org/x/drivers/ssd1306"
	"tinygo.org/x/drivers/ws2812"
)

const dmaDescriptors = 2

var disp *ssd1306.Device

var last = newBuf(100)
var adc machine.ADC

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

func blink() {
	led := machine.LED
	led.Configure(machine.PinConfig{Mode: machine.PinOutput})
	led.High()
	time.Sleep(100 * time.Millisecond)
	led.Low()
	time.Sleep(100 * time.Millisecond)
}

func main() {
	// turn off the NeoPixel
	machine.NEOPIXEL.Configure(machine.PinConfig{Mode: machine.PinOutput})
	np := ws2812.New(machine.NEOPIXEL)
	_ = np.WriteColors([]color.RGBA{{}})

	time.Sleep(time.Second)
	println("start")
	blink()
	err := machine.I2C0.Configure(machine.I2CConfig{
		SCL:       machine.I2C0_SCL_PIN,
		SDA:       machine.I2C0_SDA_PIN,
		Frequency: machine.MHz,
	})
	if err != nil {
		panic(err)
	}
	blink()

	err = oledSPI.Configure(machine.SPIConfig{
		SCK:       oledSCK,
		SDO:       oledMOSI,
		SDI:       machine.NoPin,
		Frequency: 20 * machine.MHz,
	})
	if err != nil {
		panic(err)
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

	// disp = ssd1306.NewI2CDMA(machine.I2C0, &ssd1306.DMAConfig{
	// 	DMADescriptor: (*ssd1306.DMADescriptor)(&DMADescriptorSection[1]),
	// 	DMAChannel:    1,
	// 	TriggerSource: 0x0F, // SERCOM5_DMAC_ID_TX
	// })
	// disp.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	// i2cInt := interrupt.New(sam.IRQ_DMAC_1, dispDMAInt)
	// i2cInt.SetPriority(0xC0)
	// i2cInt.Enable()
	// blink()
	// disp.ClearDisplay()
	// blink()

	disp = ssd1306.NewSPIDMA(&oledSPI, oledDC, oledRST, oledCS, &ssd1306.DMAConfig{
		DMADescriptor: (*ssd1306.DMADescriptor)(&DMADescriptorSection[1]),
		DMAChannel:    1,
		TriggerSource: 0x05, // SERCOM0_DMAC_ID_TX
	})
	// disp.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	disp.Configure(ssd1306.Config{Width: 128, Height: 64, VccState: ssd1306.SWITCHCAPVCC})
	i2cInt := interrupt.New(sam.IRQ_SERCOM0_1, dispDMAInt)
	i2cInt.SetPriority(0xC0)
	i2cInt.Enable()
	blink()
	disp.ClearDisplay()
	blink()

	// dd := ssd1306.NewI2C(machine.I2C0)
	// dd := ssd1306.NewSPI(oledSPI, oledDC, oledRST, oledCS)
	// disp = &dd
	// disp.Configure(ssd1306.Config{
	// 	Width:  128,
	// 	Height: 64,
	// 	// Address: 0x3D,
	// 	VccState: ssd1306.SWITCHCAPVCC,
	// })
	// blink()
	//
	// disp.ClearDisplay()
	// blink()

	buf, err := textbuf.New(disp, textbuf.FontSize6x8)
	if err != nil {
		panic(err)
	}

	buf.AutoFlush = true
	buf.Println("playground boot")
	println("boot")
	for disp.Busy() {
	}

	// err = matrixSPI.Configure(machine.SPIConfig{
	// 	SDI:       matrixSDI,
	// 	SDO:       matrixSDO,
	// 	SCK:       matrixSCK,
	// 	Frequency: 12 * machine.MHz,
	// })
	//
	// rgb := hub75.New(hub75.Config{
	// 	DeviceConfig: hub75.DeviceConfig{
	// 		Bus:                   &matrixSPI,
	// 		TriggerSource:         0x0D, // SERCOM4_DMAC_ID_TX
	// 		OETimerCounterControl: sam.TCC3,
	// 		TimerChannel:          0,
	// 		TimerIntenset:         sam.TCC_INTENSET_MC0,
	// 		DMAChannel:            0,
	// 		DMADescriptor:         (*hub75.DmaDescriptor)(&DMADescriptorSection[0]),
	// 	},
	// 	Data:         matrixSDO,
	// 	Clock:        matrixSCK,
	// 	Latch:        machine.PB06,
	// 	OutputEnable: machine.HUB75_OE,
	// 	A:            machine.PB00,
	// 	B:            machine.PB02,
	// 	C:            machine.PB03,
	// 	D:            machine.PB05,
	// 	Brightness:   0x20,
	// 	NumScreens:   4, // screens are 32x32 as far as this driver is concerned
	// })
	// spiInt := interrupt.New(sam.IRQ_SERCOM4_1, hub75.SPIHandler)
	// spiInt.SetPriority(0xC0)
	// spiInt.Enable()
	// rgbTimerInt := interrupt.New(sam.IRQ_TCC3_MC0, hub75.TimerHandler)
	// rgbTimerInt.SetPriority(0xC0)
	// rgbTimerInt.Enable()
	//
	// for x := int16(0); x < 128; x++ {
	// 	for y := int16(0); y < 32; y++ {
	// 		rgb.SetPixel(x, y, color.RGBA{R: 0xFF})
	// 	}
	// }
	// err = rgb.Display()
	// if err != nil {
	// 	panic(err)
	// }

	// prox := apds9960.New(machine.I2C0)
	// prox.Configure(apds9960.Configuration{})
	// println("make prox")
	// if prox.Connected() {
	// 	println("prox connected")
	// }
	// if prox.ProximityAvailable() {
	// 	println("prox available")
	// }
	// prox.EnableProximity()

	// accel := lis3dh.New(machine.I2C0)
	// accel.Address = 0x19
	// accel.Configure()

	buf.PrintlnInverse("inverse")
	w, h := buf.Size()
	buf.Println(fmt.Sprintf("w, h = %d, %d", w, h))
	buf.SetLineInverse(5, "more inverse")

	mem, mem2 := runtime.MemStats{}, runtime.MemStats{}
	runtime.ReadMemStats(&mem)
	buf.SetLine(0, time.Now().Format("03:04"), " ", strconv.Itoa(60), "Hz ", strconv.Itoa(int(mem.HeapIdle/1024)), "k/", "178", "k")
	runtime.ReadMemStats(&mem2)
	println("line 0", mem.HeapIdle-mem2.HeapIdle)
	mem = mem2
	buf.SetLine(1, time.Now().Format("03:04")+" "+strconv.Itoa(60)+"Hz "+strconv.Itoa(int(mem.HeapIdle/1024))+"k/"+"178"+"k")
	runtime.ReadMemStats(&mem2)
	println("line 1", mem.HeapIdle-mem2.HeapIdle)
	mem = mem2
	buf.SetLine(2, fmt.Sprintf("%s %dHz %dk/%sk", time.Now().Format("03:04"), 60, mem.HeapIdle/1024, "178"))
	runtime.ReadMemStats(&mem2)
	println("line 2", mem.HeapIdle-mem2.HeapIdle)

	machine.InitADC()
	// adc := machine.ADC{Pin: machine.A0}
	// adc.Configure(machine.ADCConfig{
	// 	Reference:  0,
	// 	Resolution: 10,
	// 	Samples:    2,
	// })
	// m := mic.New(machine.A0)

	adc = machine.ADC{Pin: machine.A0}
	adc.Configure(machine.ADCConfig{
		Resolution: 12,
		Samples:    1,
	})
	// sam.ADC0.REFCTRL.SetBits(sam.ADC_REFCTRL_REFSEL_AREFB)
	// sam.ADC0.SetCTRLB_FREERUN(1)

	// last := newBuf(20)

	i := interrupt.New(sam.IRQ_TC0, timerInt)
	i.Enable()

	println("config timer")
	sam.MCLK.SetAPBAMASK_TC0_(1)
	sam.GCLK.PCHCTRL[sam.PCHCTRL_GCLK_TC0].Set(sam.GCLK_PCHCTRL_GEN_GCLK1 | 1<<sam.GCLK_PCHCTRL_CHEN_Pos)
	for sam.GCLK.SYNCBUSY.Get() != 0 {
	}

	tc := sam.TC0_COUNT16
	tc.SetCTRLA_ENABLE(0)
	tc.WAVE.Set(sam.TC_COUNT16_WAVE_WAVEGEN_MFRQ)
	for tc.SYNCBUSY.Get() != 0 {
	}
	// enable interrupt
	tc.SetINTENSET_MC0(1)
	tc.CC[0].Set(0xFFFF)
	// one-shot, count down
	// tc.CTRLBSET.SetBits(sam.TC_COUNT16_CTRLBSET_ONESHOT | sam.TC_COUNT16_CTRLBSET_DIR)
	// for tc.SYNCBUSY.HasBits(sam.TC_COUNT16_SYNCBUSY_CTRLB) {
	// }
	startTimer()

	for {
		time.Sleep(time.Second / 60)
		// v := adc.Get()
		// m := last.Add(v)
		d := last.StdDev()
		fmt.Printf("%f\n", d)
		_ = buf.Println(fmt.Sprintf("%f", d))
		// fmt.Printf("%d\t%f\t%f\n", v, m, d)

		// time.Sleep(50 * time.Millisecond)
		// blink()
		// for disp.Busy() {
		// }
		// p := prox.ReadProximity()
		// buf.SetLine(7, fmt.Sprintf("prox: %d", p))
		// println(accel.ReadAcceleration())

		// m.Update()
		// println(m.Get())
	}
}

func startTimer() {
	println("starting timer")
	tc := sam.TC0_COUNT16
	// int compareValue = (int)(GCLK1_HZ / (prescaler/((float)period / 1000000))) - 1;
	// i := 48_000_000 / (1 / (64 / 1000000.0))
	tc.CC[0].Set(3072)
	for tc.SYNCBUSY.HasBits(sam.TC_COUNT16_SYNCBUSY_CC0 | sam.TC_COUNT16_SYNCBUSY_CC1) {
	}
	// tc.CTRLBSET.Set(sam.TC_COUNT16_CTRLBSET_CMD_RETRIGGER << sam.TC_COUNT16_CTRLBSET_CMD_Pos)
	// TC3->COUNT16.CTRLA.bit.ENABLE = 1;
	tc.SetCTRLA_ENABLE(1)
}

func timerInt(_ interrupt.Interrupt) {
	// println("tick")
	v := adc.Get()
	last.Add(v)
	tc := sam.TC0_COUNT16
	tc.SetINTFLAG_MC0(1)
}

type buffer struct {
	buf  []uint16
	mean float64
}

func newBuf(size int) *buffer {
	return &buffer{
		buf: make([]uint16, size),
	}
}

// moving average only, kind of ok with sampling 32
// func (b *buffer) Add(v uint16) float32 {
// 	prev := float32(b.buf[0])
// 	copy(b.buf, b.buf[1:])
// 	b.buf[len(b.buf)-1] = v
// 	b.mean = b.mean + (float32(v)-prev)/float32(len(b.buf))
// 	return b.mean
// }

// standard deviation
func (b *buffer) Add(v uint16) float64 {
	prev := float64(b.buf[0])
	copy(b.buf, b.buf[1:])
	b.buf[len(b.buf)-1] = v
	b.mean = b.mean + (float64(v)-prev)/float64(len(b.buf))
	return b.mean
}

func (b *buffer) StdDev() float64 {
	devSum := float64(0)
	for _, vv := range b.buf {
		dev := float64(vv) - b.mean
		devSum += dev * dev
	}

	return math.Sqrt(devSum / float64(len(b.buf)))
}

func (b *buffer) Min() uint16 {
	min := uint16(0xFFFF)
	for _, v := range b.buf {
		if v < min {
			min = v
		}
	}
	return min
}

func (b *buffer) Max() uint16 {
	max := uint16(0)
	for _, v := range b.buf {
		if v > max {
			max = v
		}
	}
	return max
}

func dispDMAInt(i interrupt.Interrupt) {
	// disp.TXComplete(i)
	disp.SPITXComplete(i)
}
