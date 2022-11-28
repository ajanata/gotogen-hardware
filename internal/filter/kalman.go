package filter

type Kalman struct {
	gain   float32
	values []float32
}

func NewKalman(size uint8) *Kalman {
	return &Kalman{
		gain:   0.2,
		values: make([]float32, 0, size),
	}
}

func (f *Kalman) SetGain(gain float32) {
	f.gain = gain
}

func (f *Kalman) Filter(value float32) float32 {
	var sum float32
	var avg float32
	invGain := 1 - f.gain

	// TODO ring buffer?
	if l := len(f.values); l < cap(f.values) {
		f.values = append(f.values, value)
	} else {
		copy(f.values, f.values[1:])
		f.values[l-1] = value
	}

	for _, v := range f.values {
		sum += v
	}
	avg = sum / float32(len(f.values))

	return (f.gain * value) + (invGain * avg)
}
