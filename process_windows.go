//go:build windows

package main

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procEnumWindows                   = user32.NewProc("EnumWindows")
	procGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procIsIconic                      = user32.NewProc("IsIconic")
	procGetWindowTextLengthW          = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procGetWindowLongW                = user32.NewProc("GetWindowLongW")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSHQueryUserNotificationState  = shell32.NewProc("SHQueryUserNotificationState")
)

const (
	gwlExStyle               = uintptr(0xFFFFFFEC) // -20 as a 32-bit LONG
	wsExToolWindow           = 0x00000080
	monitorDefaultNear       = 2
	qunsRunningD3DFullscreen = 3
)

var dpiOnce sync.Once

func ensureDPIAware() {
	dpiOnce.Do(func() {
		// PER_MONITOR_AWARE_V2 (-4) keeps window rectangles in physical pixels.
		if err := procSetProcessDpiAwarenessContext.Find(); err == nil {
			_, _, _ = procSetProcessDpiAwarenessContext.Call(^uintptr(3))
			return
		}
		if err := procSetProcessDPIAware.Find(); err == nil {
			_, _, _ = procSetProcessDPIAware.Call()
		}
	})
}

func scanProcesses() ([]Process, SystemState, error) {
	ensureDPIAware()

	windowsByPID, foregroundPID, d3d := scanWindows()
	state := SystemState{ForegroundPID: foregroundPID, D3DFullscreen: d3d}

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, state, fmt.Errorf("process snapshot: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	processes := make([]Process, 0, 256)
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		pid := entry.ProcessID
		if pid == 0 || pid == 4 {
			continue
		}
		exe := windows.UTF16ToString(entry.ExeFile[:])
		if exe == "" {
			continue
		}

		proc := Process{
			PID:    pid,
			Exe:    exe,
			Window: windowsByPID[pid],
		}
		// Only processes with a visible window can be games, so avoid opening
		// every background process just to read its path and start time.
		if proc.Window.Visible {
			proc.Path, proc.StartedAt = processDetails(pid)
		}
		processes = append(processes, proc)
	}
	return processes, state, nil
}

func processDetails(pid uint32) (string, time.Time) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", time.Time{}
	}
	defer windows.CloseHandle(handle)

	var path string
	size := uint32(1024)
	buf := make([]uint16, size)
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err == nil {
		path = windows.UTF16ToString(buf[:size])
	} else {
		size = 32768
		buf = make([]uint16, size)
		if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err == nil {
			path = windows.UTF16ToString(buf[:size])
		}
	}

	var started time.Time
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err == nil {
		started = time.Unix(0, creation.Nanoseconds())
	}
	return path, started
}

func scanWindows() (map[uint32]WindowInfo, uint32, bool) {
	result := make(map[uint32]WindowInfo)

	foreground, _, _ := procGetForegroundWindow.Call()
	var foregroundPID uint32
	if foreground != 0 {
		procGetWindowThreadProcessId.Call(foreground, uintptr(unsafe.Pointer(&foregroundPID)))
	}

	callback := windows.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if length, _, _ := procGetWindowTextLengthW.Call(hwnd); length == 0 {
			return 1
		}
		style, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
		if style&wsExToolWindow != 0 {
			return 1
		}

		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 {
			return 1
		}

		var text [512]uint16
		n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
		if n == 0 {
			return 1
		}
		title := windows.UTF16ToString(text[:n])

		var bounds rect
		if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
			return 1
		}
		width := int(bounds.Right - bounds.Left)
		height := int(bounds.Bottom - bounds.Top)
		if width <= 0 || height <= 0 {
			return 1
		}

		fullscreen := false
		if iconic, _, _ := procIsIconic.Call(hwnd); iconic == 0 {
			monitor, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultNear)
			if monitor != 0 {
				var info monitorInfo
				info.CbSize = uint32(unsafe.Sizeof(info))
				if ok, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ok != 0 {
					const tolerance = 2
					fullscreen = bounds.Left <= info.RcMonitor.Left+tolerance &&
						bounds.Top <= info.RcMonitor.Top+tolerance &&
						bounds.Right >= info.RcMonitor.Right-tolerance &&
						bounds.Bottom >= info.RcMonitor.Bottom-tolerance
				}
			}
		}

		isForeground := hwnd == foreground
		current, exists := result[pid]
		area := width * height
		replaceTitle := !exists || isForeground || fullscreen ||
			area > current.Width*current.Height
		if !exists {
			result[pid] = WindowInfo{
				Title:      title,
				Visible:    true,
				Foreground: isForeground,
				Fullscreen: fullscreen,
				Width:      width,
				Height:     height,
			}
			return 1
		}
		if replaceTitle {
			current.Title = title
			current.Width = width
			current.Height = height
		}
		current.Foreground = current.Foreground || isForeground
		current.Fullscreen = current.Fullscreen || fullscreen
		current.Visible = true
		result[pid] = current
		return 1
	})

	_, _, _ = procEnumWindows.Call(callback, 0)

	var notificationState int32
	hr, _, _ := procSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&notificationState)))
	d3dFullscreen := hr == 0 && notificationState == qunsRunningD3DFullscreen

	return result, foregroundPID, d3dFullscreen
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}
