param(
    [string]$CaPath = "./server-certs",
    [string]$OutputPath = "./client-certs",
    [string]$DeviceId = "device-12345"
)

Write-Host "---------------------------------------------" -ForegroundColor Yellow
Write-Host "Generacion de certificados lado cliente" -ForegroundColor Yellow
Write-Host "---------------------------------------------" -ForegroundColor Yellow

$CaCert = "$CaPath/ca.pem"
$CaKey = "$CaPath/ca-key.pem"

if (!(Test-Path $CaCert)) {
    Write-Host "[ERROR] No se encontro $CaCert" -ForegroundColor Red
    exit 1
}

if (!(Test-Path $CaKey)) {
    Write-Host "[ERROR] No se encontro $CaKey" -ForegroundColor Red
    exit 1
}

if (!(Test-Path $OutputPath)) {
    New-Item -ItemType Directory -Path $OutputPath | Out-Null
    Write-Host "[OK] Directorio $OutputPath creado"
}

$Timestamp = Get-Date -Format "yyyyMMddHHmmss"

$ClientKey = "$OutputPath/client-$DeviceId-$Timestamp-key.pem"
$ClientCsr = "$OutputPath/client-$DeviceId-$Timestamp.csr"
$ClientCert = "$OutputPath/client-$DeviceId-$Timestamp.pem"

Write-Host "1. Generando clave privada del cliente..."
openssl genrsa -out $ClientKey 2048 2>$null

if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] Fallo al generar la clave privada" -ForegroundColor Red
    exit 1
}

Write-Host "2. Generando CSR del cliente..."
openssl req -new `
    -key $ClientKey `
    -out $ClientCsr `
    -subj "/CN=$DeviceId" 2>$null

if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] Fallo al generar el CSR" -ForegroundColor Red
    exit 1
}

Write-Host "3. Firmando certificado con la CA del servidor..."
openssl x509 -req -days 365 `
    -in $ClientCsr `
    -CA $CaCert `
    -CAkey $CaKey `
    -CAcreateserial `
    -out $ClientCert 2>$null

if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] Fallo al firmar el certificado" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "================================================[EXITO]================================================" -ForegroundColor Green
Write-Host "Certificado cliente generado correctamente" -ForegroundColor Green
Write-Host "Device ID: $DeviceId" -ForegroundColor Green
Write-Host "===================================================================================================" -ForegroundColor Green
Write-Host ""

Write-Host "CA utilizada:"
Write-Host "   - $CaCert"
Write-Host ""

Write-Host "Archivos generados:"
Write-Host "   - $(Split-Path $ClientKey -Leaf)"
Write-Host "   - $(Split-Path $ClientCsr -Leaf)"
Write-Host "   - $(Split-Path $ClientCert -Leaf)"
Write-Host ""

Write-Host "Serial del certificado:"
openssl x509 -in $ClientCert -noout -serial

Write-Host ""
Write-Host "Subject del certificado:"
openssl x509 -in $ClientCert -noout -subject