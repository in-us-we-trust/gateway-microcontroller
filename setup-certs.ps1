param(
    [string]$Path = "./certs"
)

Write-Host "---------------------------------------------" -ForegroundColor Yellow
Write-Host "Generacion de certificados lado servidor" -ForegroundColor Yellow
Write-Host "---------------------------------------------" -ForegroundColor Yellow

if (!(Test-Path $Path)) {
    New-Item -ItemType Directory -Path $Path | Out-Null
    Write-Host "[OK] Directorio $Path creado"
}

Write-Host "1. Generando CA privada..."
openssl genrsa -out "$Path/ca-key.pem" 2048 2>$null

Write-Host "2. Generando certificado CA autofirmado..."
openssl req -new -x509 -days 365 `
  -key "$Path/ca-key.pem" `
  -out "$Path/ca.pem" `
  -subj "/CN=Gateway-CA" 2>$null

Write-Host "3. Generando servidor privado..."
openssl genrsa -out "$Path/server-key.pem" 2048 2>$null

Write-Host "4. Generando request certificado servidor..."
openssl req -new `
  -key "$Path/server-key.pem" `
  -out "$Path/server.csr" `
  -subj "/CN=localhost" 2>$null

Write-Host "5. Firmando certificado servidor..."
openssl x509 -req -days 365 `
  -in "$Path/server.csr" `
  -CA "$Path/ca.pem" `
  -CAkey "$Path/ca-key.pem" `
  -CAcreateserial `
  -out "$Path/server.pem" 2>$null

Write-Host "---------------------------------------------" -ForegroundColor Yellow
Write-Host "Generacion de certificados lado cliente" -ForegroundColor Yellow
Write-Host "---------------------------------------------" -ForegroundColor Yellow

Write-Host "1. Generando cliente privado..."
openssl genrsa -out "$Path/client-key.pem" 2048 2>$null

Write-Host "2. Generando request certificado cliente..."
openssl req -new `
  -key "$Path/client-key.pem" `
  -out "$Path/client.csr" `
  -subj "/CN=device-12345" 2>$null

Write-Host "3. Firmando certificado cliente..."
openssl x509 -req -days 365 `
  -in "$Path/client.csr" `
  -CA "$Path/ca.pem" `
  -CAkey "$Path/ca-key.pem" `
  -CAcreateserial `
  -out "$Path/client.pem"

Write-Host ""
Write-Host "================================================[EXITO]================================================" -ForegroundColor Green
Write-Host "Certificados generados exitosamente en $Path" -ForegroundColor Green
Write-Host "===================================================================================================" -ForegroundColor Green

Get-ChildItem $Path -File | ForEach-Object {
    Write-Host "   - $($_.Name)"
}