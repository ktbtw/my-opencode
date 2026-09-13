//go:build !darwin && !linux && !windows

package autostart

import (
	"errors"
	"runtime"
)

func platformStatus() Info {
	return Info{Method: runtime.GOOS, Error: "当前平台暂不支持开机自启动"}
}

func platformEnable(Options) (Info, error) {
	return Info{}, errors.New("当前平台暂不支持开机自启动")
}

func platformDisable() (Info, error) {
	return Info{}, errors.New("当前平台暂不支持开机自启动")
}
