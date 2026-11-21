//go:build linux || darwin

package node

import "syscall"

// diskSpace liefert den freien und den gesamten Platz des Dateisystems, auf dem path liegt (0, 0 = unbekannt).
// Frei ist, was ein normaler Benutzer noch schreiben darf (ohne die Reserve für root).
func diskSpace(path string) (free, total int64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	return int64(stat.Bavail) * int64(stat.Bsize), int64(stat.Blocks) * int64(stat.Bsize)
}
