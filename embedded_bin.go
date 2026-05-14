package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Árbol completo de bin (exe + DLL) embebido en el binario; dcmtk carga DLLs junto al .exe.
//
//go:embed all:bin
var embeddedBin embed.FS

var (
	embeddedBinDir    string
	embeddedBinDirErr error
	embeddedBinOnce   sync.Once
)

// ensureEmbeddedBinDir escribe una copia de los archivos embebidos y devuelve esa ruta.
func ensureEmbeddedBinDir() (string, error) {
	embeddedBinOnce.Do(func() {
		embeddedBinDir, embeddedBinDirErr = materializeEmbeddedBin()
	})
	return embeddedBinDir, embeddedBinDirErr
}

func materializeEmbeddedBin() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	outDir := filepath.Join(base, "DicomSender", "embedded-bin")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}

	err = fs.WalkDir(embeddedBin, "bin", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, err := embeddedBin.ReadFile(path)
		if err != nil {
			return err
		}
		rel := filepath.FromSlash(strings.TrimPrefix(path, "bin/"))
		dest := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		
		// Optimización maestra: Si el archivo ya existe y tiene el mismo tamaño, evitamos reescribir en disco
		if fi, err := os.Stat(dest); err == nil && fi.Size() == int64(len(data)) {
			return nil
		}
		
		// En Windows 0644 es suficiente, el S.O. determina la ejecución por la extensión (.exe)
		return os.WriteFile(dest, data, 0644)
	})
	if err != nil {
		return "", err
	}
	return outDir, nil
}