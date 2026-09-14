package agent

type tokenCalibration struct {
	ratio float64
}

const calibrationInitialRatio = 1.0

const calibrationAlpha = 0.5

const (
	minCalibrationRatio = 0.25
	maxCalibrationRatio = 4
)

func newTokenCalibration() *tokenCalibration {
	return &tokenCalibration{ratio: calibrationInitialRatio}
}

func (c *tokenCalibration) observe(real, estimated int) {
	if real <= 0 || estimated <= 0 {
		return
	}
	sample := float64(real) / float64(estimated)
	if sample < minCalibrationRatio || sample > maxCalibrationRatio {
		return
	}
	c.ratio = calibrationAlpha*sample + (1-calibrationAlpha)*c.ratio
}

func (c *tokenCalibration) current() float64 {
	return c.ratio
}
