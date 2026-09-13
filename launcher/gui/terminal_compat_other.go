//go:build !darwin

package main

import (
	"errors"

	"launcher/internal/config"
)

func terminalCompatSupported() bool {
	return false
}

func terminalCompatStartBackground(cfg config.Config, executable string) error {
	return errors.New("当前系统不支持终端兼容模式")
}

func terminalCompatAutostartOptions(cfg config.Config, executable string) (string, []string, error) {
	return "", nil, errors.New("当前系统不支持终端兼容模式")
}
