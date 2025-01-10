package internal

import "os"

// GetProcPath returns the path stored in HOST_PROC env variable, or /proc if HOST_PROC has not been set.
func GetProcPath() string {
	if hostProc := os.Getenv("HOST_PROC"); hostProc != "" {
		return hostProc
	}
	return "/proc"
}

// GetSysPath returns the path stored in HOST_SYS env variable, or /sys if HOST_SYS has not been set.
func GetSysPath() string {
	if hostSys := os.Getenv("HOST_SYS"); hostSys != "" {
		return hostSys
	}
	return "/sys"
}
