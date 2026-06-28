package middleware

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"errors"
	"log"
	"strings"

	"github.com/gin-gonic/gin"
)

var errInvalidSubject = errors.New("invalid client certificate subject")

func ExtractDeviceIDFromMTLS(c *gin.Context) (string, error) {
	subject := strings.TrimSpace(c.GetHeader("X-Client-Cert-Subject"))
	if subject == "" {
		log.Printf("[security] missing X-Client-Cert-Subject header")
		return "", errors.New("client certificate subject header is required")
	}

	deviceID, err := extractDeviceIDFromSubject(subject)
	if err != nil {
		log.Printf("[security] failed to extract device ID from client certificate subject: %v", err)
		return "", err
	}

	log.Printf("[security] authenticated device %q via mTLS", deviceID)

	return deviceID, nil
}

func extractDeviceIDFromSubject(encoded string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errInvalidSubject
	}

	var rdn pkix.RDNSequence
	if _, err := asn1.Unmarshal(der, &rdn); err != nil {
		return "", errInvalidSubject
	}

	var name pkix.Name
	name.FillFromRDNSequence(&rdn)

	deviceID := strings.TrimSpace(name.CommonName)
	if deviceID == "" {
		return "", errInvalidSubject
	}

	return deviceID, nil
}
