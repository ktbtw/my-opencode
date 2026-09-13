//go:build windows

package projectidentity

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsFileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

func directoryIdentity(root string) (canonical, volumeID, fileID string, err error) {
	canonical, err = filepath.Abs(root)
	if err != nil {
		return "", "", "", err
	}
	pathPtr, err := windows.UTF16PtrFromString(canonical)
	if err != nil {
		return "", "", "", err
	}
	handle, err := windows.CreateFile(pathPtr, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", "", "", err
	}
	defer windows.CloseHandle(handle)
	var info windowsFileIDInfo
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return "", "", "", err
	}
	buffer := make([]uint16, 32*1024)
	length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", "", "", err
	}
	if length == 0 || int(length) >= len(buffer) {
		return "", "", "", fmt.Errorf("canonical project path exceeds Windows path buffer")
	}
	canonical = windows.UTF16ToString(buffer[:length])
	if strings.HasPrefix(canonical, `\\?\UNC\`) {
		canonical = `\\` + strings.TrimPrefix(canonical, `\\?\UNC\`)
	} else {
		canonical = strings.TrimPrefix(canonical, `\\?\`)
	}
	volumeID = fmt.Sprintf("%016x", info.VolumeSerialNumber)
	fileID = hex.EncodeToString(info.FileID[:])
	return filepath.Clean(canonical), volumeID, fileID, nil
}
