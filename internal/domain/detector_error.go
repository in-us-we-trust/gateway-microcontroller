package domain

import "fmt"

const (
	ErrCodeDetectorAnalysis = "DTE-001"
	ErrCodeMediaDispatch    = "DTE-002"
)

type DetectorError struct {
	Code           string
	DetectorDetail string
	StatusCode     int
}

func (e *DetectorError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.DetectorDetail)
}
