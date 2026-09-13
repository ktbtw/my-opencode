//go:build !windows

package app

func prepareIDAEULAState(_ string, _ string) (string, error) {
	return "", nil
}
