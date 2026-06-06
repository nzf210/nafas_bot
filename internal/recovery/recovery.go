// ============================================================
// MODULE: recovery
// Deskripsi: Panic recovery utilities untuk goroutine safety
// ============================================================

package recovery

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/nzf210/nafas-bot/internal/logger"
)

// PanicHandler recovers from panics and logs them
// Nama Function: PanicHandler
// Deskripsi: Function yang dipanggil dengan defer untuk recover dari panic.
// Parameter/Value Input:
//   - Tidak ada parameter langsung, menggunakan recover()
// Function yang Dipanggil/Dikonsumsi:
//   - logger.Default: dipanggil untuk logging error
//   - debug.Stack: dipanggil untuk ambil stack trace
// Output/Return Value:
//   - Tidak ada return value langsung, hanya recover dari panic
// Catatan: Harus digunakan dengan defer PanicHandler() di awal goroutine
func PanicHandler() {
	if r := recover(); r != nil {
		logger := logger.Default()
		stack := debug.Stack()

		// Log panic dengan stack trace
		logger.Errorf("PANIC recovered: %v\n%s", r, string(stack))

		// Also write to stderr for visibility
		fmt.Fprintf(os.Stderr, "PANIC recovered: %v\n%s\n", r, string(stack))
	}
}

// SafeGo runs a function with panic recovery
// Nama Function: SafeGo
// Deskripsi: Menjalankan function dengan panic recovery otomatis.
// Parameter/Value Input:
//   - fn: func() — function yang akan dijalankan
//   - name: string — nama goroutine untuk logging
// Function yang Dipanggil/Dikonsumsi:
//   - PanicHandler: dipanggil via defer untuk recover
// Output/Return Value:
//   - Tidak ada return value langsung
func SafeGo(fn func(), name string) {
	go func() {
		defer PanicHandler()
		fn()
	}()
}

// WithRecovery wraps a function with panic recovery
// Nama Function: WithRecovery
// Deskripsi: Membuat function wrapper yang auto-recover dari panic.
// Parameter/Value Input:
//   - fn: func() — function yang akan dibungkus
//   - name: string — nama untuk logging
// Function yang Dipanggil/Dikonsumsi:
//   - PanicHandler: dipanggil via defer untuk recover
// Output/Return Value:
//   - func(): function baru yang sudah dibungkus dengan panic recovery
func WithRecovery(fn func(), name string) func() {
	return func() {
		defer PanicHandler()
		fn()
	}
}