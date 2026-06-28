package domain

type DeviceRequest struct {
	ID                 string  `json:"id" swaggerignore:"true"`
	SerialNumber       string  `json:"serialNumber" binding:"required" example:"ESP32-000001"`
	City               string  `json:"city,omitempty" example:"San Miguel"`
	LastSeen           string  `json:"lastSeen" binding:"required" example:"2026-06-27T18:30:00Z"`
	LastBoot           string  `json:"lastBoot" binding:"required" example:"2026-06-27T17:45:00Z"`
	DetectionThreshold float64 `json:"detectionThreshold,omitempty" example:"85"`
	Location           string  `json:"location,omitempty" example:"Front door"`
	FirmwareVersion    string  `json:"firmwareVersion,omitempty" example:"v1.3.7"`
}

type DeviceResponse struct {
	Data DeviceData `json:"data"`
}

type DeviceData struct {
	ID                 string  `json:"id" example:"9df4a749-3ec2-43b9-bc42-35d2b6b5b173"`
	SerialNumber       string  `json:"serialNumber" example:"ESP32-000001"`
	City               string  `json:"city" example:"San Miguel"`
	LastSeen           string  `json:"lastSeen" example:"2026-06-27T18:30:00Z"`
	LastBoot           string  `json:"lastBoot" example:"2026-06-27T17:45:00Z"`
	Status             string  `json:"status" example:"UNCLAIMED"`
	Uptime             int64   `json:"uptime" example:"2700"`
	DetectionThreshold float64 `json:"detectionThreshold" example:"85"`
	Location           string  `json:"location" example:"Main Entrance"`
	FirmwareVersion    string  `json:"firmwareVersion" example:"v1.3.7"`
}
