package app

import (
	"launcher/internal/storage"
)

// StorageUsage 按需统计运行目录的磁盘占用。
//
// 目录体积遍历开销较大，因此只在界面主动查询时执行，不放进心跳。
func (s *service) StorageUsage() (storage.Usage, error) {
	return storage.Collect(s.cfg.RuntimeDir)
}

// ClearStorage 清理指定的可清理类别；keys 为空时清理全部可清理类别。
// 运行时组件、Agent 数据与程序文件不会被清理，详见 storage 包的类别定义。
func (s *service) ClearStorage(keys []string) (storage.ClearResult, error) {
	return storage.Clear(s.cfg.RuntimeDir, keys)
}
