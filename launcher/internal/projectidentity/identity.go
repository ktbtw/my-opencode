package projectidentity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const markerSchemaVersion = 2

const (
	identityLockRetry = 25 * time.Millisecond
	identityLockWait  = 10 * time.Second
	identityLockStale = 2 * time.Minute
)

type Identity struct {
	ScopeID        string `json:"project_scope_id"`
	InstanceNonce  string `json:"instance_nonce"`
	LineageScopeID string `json:"lineage_project_scope_id,omitempty"`
	MachineID      string `json:"machine_id"`
	Root           string `json:"root"`
	DisplayName    string `json:"display_name"`
	VolumeID       string `json:"filesystem_volume_id,omitempty"`
	FileID         string `json:"filesystem_file_id,omitempty"`
	MarkerWritable bool   `json:"marker_writable"`
}

type markerInstance struct {
	ScopeID        string    `json:"project_scope_id"`
	InstanceNonce  string    `json:"instance_nonce"`
	LineageScopeID string    `json:"lineage_project_scope_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type markerFile struct {
	SchemaVersion int                       `json:"schema_version"`
	LineageNonce  string                    `json:"lineage_nonce"`
	Instances     map[string]markerInstance `json:"instances"`
}

type registryRecord struct {
	ScopeID        string    `json:"project_scope_id"`
	InstanceNonce  string    `json:"instance_nonce"`
	LineageScopeID string    `json:"lineage_project_scope_id,omitempty"`
	Root           string    `json:"root"`
	VolumeID       string    `json:"filesystem_volume_id,omitempty"`
	FileID         string    `json:"filesystem_file_id,omitempty"`
	LastSeenAt     time.Time `json:"last_seen_at"`
}

type registryFile struct {
	SchemaVersion int                         `json:"schema_version"`
	Machines      map[string][]registryRecord `json:"machines"`
}

func Resolve(runtimeDir, machineID, root string) (Identity, error) {
	var identity Identity
	err := withIdentityLock(runtimeDir, func() error {
		var err error
		identity, err = resolveUnlocked(runtimeDir, machineID, root)
		return err
	})
	return identity, err
}

func resolveUnlocked(runtimeDir, machineID, root string) (Identity, error) {
	return resolveUnlockedWithDirectoryIdentity(runtimeDir, machineID, root, directoryIdentity)
}

type directoryIdentityFunc func(string) (canonical, volumeID, fileID string, err error)

func resolveUnlockedWithDirectoryIdentity(runtimeDir, machineID, root string, identify directoryIdentityFunc) (Identity, error) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return Identity{}, errors.New("machine id is required")
	}
	canonical, volumeID, fileID, err := identify(root)
	if err != nil {
		return Identity{}, err
	}
	root = canonical
	markerPath := filepath.Join(root, ".chat-codex", "project.json")
	marker, markerErr := readMarker(markerPath)
	if markerErr != nil && !os.IsNotExist(markerErr) {
		return Identity{}, markerErr
	}
	if marker.Instances == nil {
		marker.Instances = map[string]markerInstance{}
	}
	if marker.SchemaVersion == 0 {
		marker.SchemaVersion = markerSchemaVersion
	}
	if marker.LineageNonce == "" {
		marker.LineageNonce = randomID(16)
	}
	registryPath := filepath.Join(runtimeDir, "project_identity.json")
	registry, err := readRegistry(registryPath)
	if err != nil {
		return Identity{}, err
	}
	if registry.Machines == nil {
		registry.Machines = map[string][]registryRecord{}
	}
	instance, exists := marker.Instances[machineID]
	if !exists || instance.ScopeID == "" || instance.InstanceNonce == "" {
		lineageScopeID := ""
		for otherMachine, other := range marker.Instances {
			if otherMachine != machineID && other.ScopeID != "" {
				lineageScopeID = other.ScopeID
				break
			}
		}
		for _, record := range registry.Machines[machineID] {
			if sameFilesystemObject(record, volumeID, fileID) ||
				((volumeID == "" || fileID == "") && filepath.Clean(record.Root) == root) {
				instance = markerInstance{ScopeID: record.ScopeID, InstanceNonce: record.InstanceNonce, LineageScopeID: record.LineageScopeID}
				exists = true
				break
			}
		}
		if !exists || instance.ScopeID == "" || instance.InstanceNonce == "" {
			instance = newMarkerInstance(lineageScopeID)
		}
		marker.Instances[machineID] = instance
	}
	records := registry.Machines[machineID]
	matched := -1
	for i := range records {
		if records[i].ScopeID == instance.ScopeID && records[i].InstanceNonce == instance.InstanceNonce {
			matched = i
			break
		}
	}
	if matched >= 0 && !sameFilesystemObject(records[matched], volumeID, fileID) {
		if oldLocationStillOwns(records[matched], identify) {
			instance = newMarkerInstance(instance.ScopeID)
			marker.Instances[machineID] = instance
			matched = -1
		}
	}
	now := time.Now().UTC()
	record := registryRecord{
		ScopeID: instance.ScopeID, InstanceNonce: instance.InstanceNonce, Root: root,
		LineageScopeID: instance.LineageScopeID, VolumeID: volumeID, FileID: fileID, LastSeenAt: now,
	}
	if matched >= 0 {
		records[matched] = record
	} else {
		records = append(records, record)
	}
	registry.Machines[machineID] = records
	markerWritable := writeMarker(markerPath, marker) == nil
	if err := writeJSONAtomic(registryPath, registry, 0o600); err != nil {
		return Identity{}, err
	}
	if markerWritable {
		_ = excludeMarkerFromGit(root)
	}
	return Identity{
		ScopeID: instance.ScopeID, InstanceNonce: instance.InstanceNonce, LineageScopeID: instance.LineageScopeID, MachineID: machineID,
		Root: root, DisplayName: filepath.Base(root), VolumeID: volumeID, FileID: fileID,
		MarkerWritable: markerWritable,
	}, nil
}

func newMarkerInstance(lineageScopeID string) markerInstance {
	return markerInstance{
		ScopeID: "proj_" + randomID(16), InstanceNonce: randomID(16),
		LineageScopeID: strings.TrimSpace(lineageScopeID), CreatedAt: time.Now().UTC(),
	}
}

// Correct rebinds one folder identity. "keep_memory_here" keeps the supplied
// scope and gives this folder a fresh instance; "treat_as_new" forks it.
func Correct(runtimeDir, machineID, root, action, keepScopeID string) (Identity, error) {
	var identity Identity
	err := withIdentityLock(runtimeDir, func() error {
		current, err := resolveUnlocked(runtimeDir, machineID, root)
		if err != nil {
			return err
		}
		var next markerInstance
		switch strings.TrimSpace(action) {
		case "keep_memory_here":
			keepScopeID = strings.TrimSpace(keepScopeID)
			if keepScopeID == "" {
				return errors.New("keep scope id is required")
			}
			next = markerInstance{ScopeID: keepScopeID, InstanceNonce: randomID(16), CreatedAt: time.Now().UTC()}
		case "treat_as_new":
			next = newMarkerInstance(current.ScopeID)
		default:
			return errors.New("invalid identity correction action")
		}
		identity, err = writeCorrectedIdentity(runtimeDir, machineID, current.Root, current.VolumeID, current.FileID, next)
		return err
	})
	return identity, err
}

func writeCorrectedIdentity(runtimeDir, machineID, root, volumeID, fileID string, next markerInstance) (Identity, error) {
	markerPath := filepath.Join(root, ".chat-codex", "project.json")
	marker, err := readMarker(markerPath)
	if err != nil && !os.IsNotExist(err) {
		return Identity{}, err
	}
	if marker.Instances == nil {
		marker.Instances = map[string]markerInstance{}
	}
	if marker.LineageNonce == "" {
		marker.LineageNonce = randomID(16)
	}
	marker.Instances[machineID] = next

	registryPath := filepath.Join(runtimeDir, "project_identity.json")
	registry, err := readRegistry(registryPath)
	if err != nil {
		return Identity{}, err
	}
	records := registry.Machines[machineID]
	filtered := records[:0]
	for _, record := range records {
		if sameFilesystemObject(record, volumeID, fileID) || filepath.Clean(record.Root) == root {
			continue
		}
		filtered = append(filtered, record)
	}
	registry.Machines[machineID] = append(filtered, registryRecord{
		ScopeID: next.ScopeID, InstanceNonce: next.InstanceNonce, LineageScopeID: next.LineageScopeID,
		Root: root, VolumeID: volumeID, FileID: fileID, LastSeenAt: time.Now().UTC(),
	})
	markerWritable := writeMarker(markerPath, marker) == nil
	if err := writeJSONAtomic(registryPath, registry, 0o600); err != nil {
		return Identity{}, err
	}
	if markerWritable {
		_ = excludeMarkerFromGit(root)
	}
	return Identity{
		ScopeID: next.ScopeID, InstanceNonce: next.InstanceNonce, LineageScopeID: next.LineageScopeID,
		MachineID: machineID, Root: root, DisplayName: filepath.Base(root), VolumeID: volumeID,
		FileID: fileID, MarkerWritable: markerWritable,
	}, nil
}

func withIdentityLock(runtimeDir string, fn func() error) error {
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return err
	}
	lockPath := filepath.Join(runtimeDir, "project_identity.lock")
	ctx, cancel := context.WithTimeout(context.Background(), identityLockWait)
	defer cancel()
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, _ = file.WriteString(randomID(8))
			_ = file.Close()
			defer os.Remove(lockPath)
			return fn()
		}
		if !os.IsExist(err) {
			return err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > identityLockStale {
			_ = os.Remove(lockPath)
			continue
		}
		select {
		case <-ctx.Done():
			return errors.New("project identity lock timeout")
		case <-time.After(identityLockRetry):
		}
	}
}

func randomID(size int) string {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		stamp := time.Now().UTC().Format("20060102150405.000000000")
		return hex.EncodeToString([]byte(stamp))
	}
	return hex.EncodeToString(buffer)
}

func readMarker(path string) (markerFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return markerFile{}, err
	}
	var marker markerFile
	if err := json.Unmarshal(data, &marker); err != nil {
		return markerFile{}, err
	}
	return marker, nil
}

func readRegistry(path string) (registryFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return registryFile{SchemaVersion: 1, Machines: map[string][]registryRecord{}}, nil
	}
	if err != nil {
		return registryFile{}, err
	}
	var registry registryFile
	if err := json.Unmarshal(data, &registry); err != nil {
		return registryFile{}, err
	}
	return registry, nil
}

func writeMarker(path string, marker markerFile) error {
	marker.SchemaVersion = markerSchemaVersion
	return writeJSONAtomic(path, marker, 0o600)
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".project-identity-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func sameFilesystemObject(record registryRecord, volumeID, fileID string) bool {
	return record.VolumeID != "" && record.FileID != "" && record.VolumeID == volumeID && record.FileID == fileID
}

func oldLocationStillOwns(record registryRecord, identify directoryIdentityFunc) bool {
	if strings.TrimSpace(record.Root) == "" {
		return false
	}
	_, volumeID, fileID, err := identify(record.Root)
	if err != nil {
		return false
	}
	return sameFilesystemObject(record, volumeID, fileID)
}

func excludeMarkerFromGit(root string) error {
	gitDir := filepath.Join(root, ".git")
	info, err := os.Stat(gitDir)
	if err != nil || !info.IsDir() {
		return nil
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")
	data, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	const entry = ".chat-codex/"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, []byte(entry+"\n")...)
	return writeFileAtomic(excludePath, data, 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".exclude-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
