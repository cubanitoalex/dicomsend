@echo off
chcp 65001 >nul
echo =============================================================
echo  Compilando DICOM Sender para Windows (GUI, sin consola)
echo =============================================================

:: Descargar dependencias
go mod tidy

:: Icono del .exe en el Explorador: Go no enlaza .ico solo por tenerlo en la carpeta;
:: hay que generar un .syso (recursos PE) con rsrc y luego go build lo incorpora.
echo.
echo  Generando recursos Windows desde logo.ico ...
for /f "tokens=*" %%G in ('go env GOARCH') do (
  go run github.com/akavel/rsrc@v0.10.2 -arch %%G -ico logo.ico -o rsrc_windows_%%G.syso
)
if %ERRORLEVEL% neq 0 (
    echo  [ERROR] No se pudo generar rsrc_windows_*.syso. Comprueba que logo.ico existe.
    pause
    exit /b 1
)

:: Compilar sin ventana de consola (-H windowsgui)
go build -ldflags="-H windowsgui -s -w" -o DicomSender.exe .

if %ERRORLEVEL%==0 (
    echo.
    echo  [OK] DicomSender.exe creado correctamente.
) else (
    echo.
    echo  [ERROR] La compilacion fallo. Revisa los mensajes anteriores.
)

pause
