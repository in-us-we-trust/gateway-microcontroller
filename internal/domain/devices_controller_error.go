package domain

type DevicesControllerError struct {
	StatusCode int
	Body       []byte
}

func (e *DevicesControllerError) Error() string {
	return "devices controller error"
}
