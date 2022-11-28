package main

import (
	"fmt"
	"machine"
	"runtime"
	"runtime/interrupt"
	"strconv"
	"time"

	"github.com/ajanata/textbuf"
	"tinygo.org/x/drivers/apds9960"
	"tinygo.org/x/drivers/lis3dh"
	"tinygo.org/x/drivers/ssd1306"

	"github.com/ajanata/gotogen-hardware/internal/mic"
)

const dmaDescriptors = 2

var disp ssd1306.Device

// //go:align 16
// var DMADescriptorSection [dmaDescriptors]DMADescriptor
//
// //go:align 16
// var DMADescriptorWritebackSection [dmaDescriptors]DMADescriptor
//
// type DMADescriptor struct {
// 	Btctrl   uint16
// 	Btcnt    uint16
// 	Srcaddr  unsafe.Pointer
// 	Dstaddr  unsafe.Pointer
// 	Descaddr unsafe.Pointer
// }

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

	// Init DMAC.
	// First configure the clocks, then configure the DMA descriptors. Those
	// descriptors must live in SRAM and must be aligned on a 16-byte boundary.
	// http://www.lucadavidian.com/2018/03/08/wifi-controlled-neo-pixels-strips/
	// https://svn.larosterna.com/oss/trunk/arduino/zerotimer/zerodma.cpp
	// sam.MCLK.AHBMASK.SetBits(sam.MCLK_AHBMASK_DMAC_)
	// sam.DMAC.BASEADDR.Set(uint32(uintptr(unsafe.Pointer(&DMADescriptorSection))))
	// sam.DMAC.WRBADDR.Set(uint32(uintptr(unsafe.Pointer(&DMADescriptorWritebackSection))))
	// // Enable peripheral with all priorities.
	// sam.DMAC.CTRL.SetBits(sam.DMAC_CTRL_DMAENABLE | sam.DMAC_CTRL_LVLEN0 | sam.DMAC_CTRL_LVLEN1 | sam.DMAC_CTRL_LVLEN2 | sam.DMAC_CTRL_LVLEN3)
	//
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

	disp = ssd1306.NewI2C(machine.I2C0)
	disp.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	blink()

	disp.ClearDisplay()
	blink()

	buf, err := textbuf.New(&disp, textbuf.FontSize6x8)
	if err != nil {
		panic(err)
	}

	buf.AutoFlush = true
	buf.Println("playground boot")
	println("boot")
	for disp.Busy() {
	}

	prox := apds9960.New(machine.I2C0)
	prox.Configure(apds9960.Configuration{})
	println("make prox")
	if prox.Connected() {
		println("prox connected")
	}
	if prox.ProximityAvailable() {
		println("prox available")
	}
	prox.EnableProximity()

	accel := lis3dh.New(machine.I2C0)
	accel.Address = 0x19
	accel.Configure()

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
	m := mic.New(machine.A0)

	for {
		// time.Sleep(50 * time.Millisecond)
		// blink()
		for disp.Busy() {
		}
		// p := prox.ReadProximity()
		// buf.SetLine(7, fmt.Sprintf("prox: %d", p))
		// println(accel.ReadAcceleration())

		m.Update()
		println(m.Get())
	}
}

func dispDMAInt(i interrupt.Interrupt) {
	disp.TXComplete(i)
}
