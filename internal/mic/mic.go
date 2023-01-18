package mic

import (
	"device/sam"
	"machine"
	"math"
	"runtime/interrupt"
)

type Mic struct {
	adc machine.ADC
	buf buffer
}

var instance *Mic

type buffer struct {
	buf  []uint16
	mean float32
}

// New creates a new mic driver for the specified analog pin.
// Creating more than one Mic is not allowed.
func New(pin machine.Pin, bufSize uint16) *Mic {
	if instance != nil {
		panic("cannot create more than one microphone driver")
	}

	adc := machine.ADC{Pin: pin}
	adc.Configure(machine.ADCConfig{
		Resolution: 12,
		Samples:    1,
	})
	sam.ADC0.SetCTRLB_FREERUN(1)

	m := &Mic{
		adc: adc,
		buf: buffer{
			buf: make([]uint16, bufSize),
		},
	}
	instance = m

	i := interrupt.New(sam.IRQ_TC0, irq)
	i.Enable()

	// configure timer
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

	// start timer
	tc.CC[0].Set(3072)
	for tc.SYNCBUSY.HasBits(sam.TC_COUNT16_SYNCBUSY_CC0 | sam.TC_COUNT16_SYNCBUSY_CC1) {
	}
	tc.SetCTRLA_ENABLE(1)

	return m
}

func (m *Mic) Value() float32 {
	return float32(m.buf.stdDev())
}

func irq(_ interrupt.Interrupt) {
	v := instance.adc.Get()
	instance.buf.add(v)
	sam.TC0_COUNT16.SetINTFLAG_MC0(1)
}

func (b *buffer) add(v uint16) float32 {
	prev := float32(b.buf[0])
	copy(b.buf, b.buf[1:])
	b.buf[len(b.buf)-1] = v
	b.mean = b.mean + (float32(v)-prev)/float32(len(b.buf))
	return b.mean
}

func (b *buffer) stdDev() float64 {
	devSum := float64(0)
	mean := float64(b.mean)
	for _, vv := range b.buf {
		dev := float64(vv) - mean
		devSum += dev * dev
	}

	return math.Sqrt(devSum / float64(len(b.buf)))
}
