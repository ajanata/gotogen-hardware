package filter

import (
	"math"
)

type Min struct {
	values     []float32
	minValues  []float32
	ignoreSame bool
}

func NewMin(size uint8, ignoreSame bool) *Min {
	return &Min{
		values:     make([]float32, 0, size),
		minValues:  make([]float32, size/10), // intentionally not 3-value, want it initialized with zeros
		ignoreSame: ignoreSame,
	}
}

func (m *Min) Filter(value float32) float32 {
	var sum float32

	// TODO ring buffer?
	if l := len(m.values); l < cap(m.values) {
		m.values = append(m.values, value)
	} else {
		copy(m.values, m.values[1:])
		m.values[l-1] = value
	}

	min := float32(math.MaxFloat32)
	for _, v := range m.values {
		if v < min {
			min = v
		}
	}

	if l := len(m.minValues); m.minValues[l-1] != min || !m.ignoreSame {
		// current min is different (or we save dupes), so save it
		copy(m.minValues, m.minValues[1:])
		m.minValues[l-1] = min
	}

	for _, v := range m.minValues {
		sum += v
	}

	return sum / float32(len(m.minValues))
}
