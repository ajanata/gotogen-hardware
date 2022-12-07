package mic

import (
	"device/sam"
	"fmt"
	"machine"
	"math"
	"time"

	"github.com/ajanata/gotogen-hardware/internal/filter"
)

type Mic struct {
	adc       machine.ADC
	mv        *filter.Kalman
	minF      *filter.Min
	out       *filter.Kalman
	prev      float32
	gain      float32
	clipping  float32
	cur       float32
	prevTime  time.Time
	startTime time.Time
}

func New(pin machine.Pin) *Mic {
	adc := machine.ADC{Pin: pin}
	adc.Configure(machine.ADCConfig{
		Reference:  0,
		Resolution: 12,
		Samples:    32,
	})
	// sam.ADC0.REFCTRL.SetBits(sam.ADC_REFCTRL_REFSEL_AREFB)
	sam.ADC0.SetCTRLB_FREERUN(1)

	return &Mic{
		adc:       adc,
		mv:        filter.NewKalman(5),
		minF:      filter.NewMin(100, true),
		out:       filter.NewKalman(5),
		gain:      .2,
		clipping:  .2,
		startTime: time.Now(),
	}
}

func (m *Mic) Get() float32 {
	return m.cur
}

func (m *Mic) Update() {
	read := float32(m.adc.Get()) * m.gain
	chg := read - m.prev
	dT := float32(time.Now().Sub(m.prevTime).Microseconds())
	m.prevTime = time.Now()
	m.prev = read
	chgRate := float64(chg / dT)
	amp := m.mv.Filter(float32(math.Abs(chgRate)) * 10_000)
	min := m.minF.Filter(amp)
	norm := amp - min - 10_000
	if norm < 0 {
		norm = 0
	} else if norm > 40_000 {
		norm = 40_000
	}
	trunc := m.out.Filter(norm / 100 / m.clipping)

	fmt.Printf("read %12f\tamp %12f\tchg %12f\tdT %12f\tnorm %12f\tmin %12f\ttrunc %12f\n", read, amp, chg, dT, norm, min, trunc)

	if trunc < 0 {
		trunc = 0
	} else if trunc > 1 {
		trunc = 1
	}
	m.cur = trunc
}
