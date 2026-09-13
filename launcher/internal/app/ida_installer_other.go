//go:build !windows

package app

import (
	"context"
	"errors"
)

func runElevatedIDAWindowsInstaller(context.Context, string, []string) (string, error) {
	return "", errors.New("Windows IDA 提权安装器仅支持 Windows")
}
