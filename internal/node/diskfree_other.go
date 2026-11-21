//go:build !linux && !darwin && !windows

package node

// diskSpace ist auf diesem System unbekannt.
func diskSpace(string) (free, total int64) { return 0, 0 }
