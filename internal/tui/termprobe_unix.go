//go:build unix

package tui

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

// probeRead liest hoechstens d Millisekunden blockierend aus f. Ohne Daten
// liefert es (0, nil) zurueck — es blockiert NIE endlos.
//
// Hintergrund: SetReadDeadline versagt auf os.Stdin, wenn fd 0 nicht im
// Go-Poller registriert ist (der Fehler wird gern ignoriert) — der Read
// haengt dann fuer immer. Der PTY-Strace des Demos zeigte exakt das:
// blockierendes read(0, ...) ohne Aufwachen, Terminal-Ausgabe 0 Bytes,
// "laedt unendlich". select(2) mit Timeout ist die klassische, robuste
// Loesung fuer Terminal-Probes.
func probeRead(f *os.File, buf []byte, d time.Duration) (int, error) {
	fd := int(f.Fd())
	var rs syscall.FdSet
	fdSet(&rs, fd)
	tv := syscall.NsecToTimeval(int64(d))
	n, err := syscall.Select(fd+1, &rs, nil, nil, &tv)
	if err != nil {
		if err == syscall.EINTR {
			return 0, nil
		}
		return 0, err
	}
	if n == 0 {
		return 0, nil // Timeout ohne Daten
	}
	return f.Read(buf)
}

// fdSet setzt ein Bit in der plattformabhaengigen FdSet-Struktur
// (Linux: [16]int64, Darwin: [32]int32 — Elementgroesse wird berechnet).
func fdSet(p *syscall.FdSet, fd int) {
	bits := int(unsafe.Sizeof(p.Bits[0])) * 8
	for i := range p.Bits {
		p.Bits[i] = 0
	}
	if fd/bits < len(p.Bits) {
		p.Bits[fd/bits] |= 1 << (uint(fd) % uint(bits))
	}
}
