package cloner

import (
	"errors"
	"fmt"
	"testing"

	"github.com/voidingwarranties/offsite-apfs-backup/diskutil"
)

type fakeDevices struct {
	// Map of volume UUID to volume info.
	volumes map[string]diskutil.VolumeInfo
	// Map of volume UUID to snapshots.
	snapshots map[string][]diskutil.Snapshot
}

type fakeDevicesOption func(*testing.T, *fakeDevices)

func withFakeVolume(info diskutil.VolumeInfo, snaps ...diskutil.Snapshot) fakeDevicesOption {
	return func(t *testing.T, d *fakeDevices) {
		t.Helper()
		if err := d.AddVolume(info, snaps...); err != nil {
			t.Fatal(err)
		}
	}
}

func newFakeDevices(t *testing.T, opts ...fakeDevicesOption) *fakeDevices {
	t.Helper()
	d := &fakeDevices{
		volumes:   make(map[string]diskutil.VolumeInfo),
		snapshots: make(map[string][]diskutil.Snapshot),
	}
	for _, opt := range opts {
		opt(t, d)
	}
	return d
}

func (d *fakeDevices) Volume(id string) (diskutil.VolumeInfo, error) {
	for _, info := range d.volumes {
		if info.UUID == id || info.Name == id || info.MountPoint == id || info.Device == id {
			return info, nil
		}
	}
	return diskutil.VolumeInfo{}, errors.New("volume does not exist")
}

func (d *fakeDevices) AddVolume(volume diskutil.VolumeInfo, snapshots ...diskutil.Snapshot) error {
	if _, exists := d.volumes[volume.UUID]; exists {
		return fmt.Errorf("volume %q already exists", volume.Name)
	}
	d.volumes[volume.UUID] = volume
	d.snapshots[volume.UUID] = snapshots
	return nil
}

func (d *fakeDevices) RemoveVolume(uuid string) error {
	if _, exists := d.volumes[uuid]; !exists {
		return errors.New("volume does not exist")
	}
	delete(d.volumes, uuid)
	delete(d.snapshots, uuid)
	return nil
}

func (d *fakeDevices) Snapshots(volumeUUID string) ([]diskutil.Snapshot, error) {
	snaps, exists := d.snapshots[volumeUUID]
	if !exists {
		return nil, errors.New("volume not found")
	}
	return snaps, nil
}

func (d *fakeDevices) Snapshot(volumeUUID, snapshotUUID string) (diskutil.Snapshot, error) {
	snaps, err := d.Snapshots(volumeUUID)
	if err != nil {
		return diskutil.Snapshot{}, err
	}
	for _, s := range snaps {
		if s.UUID == snapshotUUID {
			return s, nil
		}
	}
	return diskutil.Snapshot{}, errors.New("snapshot not found")
}

func (d *fakeDevices) AddSnapshot(volumeUUID string, snapshot diskutil.Snapshot) error {
	if _, exists := d.volumes[volumeUUID]; !exists {
		return errors.New("volume not found")
	}
	for _, s := range d.snapshots[volumeUUID] {
		if s.UUID == snapshot.UUID {
			return errors.New("snapshot already exists")
		}
	}
	d.snapshots[volumeUUID] = append(d.snapshots[volumeUUID], snapshot)
	return nil
}

func (d *fakeDevices) DeleteSnapshot(volumeUUID, snapshotUUID string) error {
	snaps, err := d.Snapshots(volumeUUID)
	if err != nil {
		return err
	}
	snapI := -1
	for i, s := range snaps {
		if s.UUID == snapshotUUID {
			snapI = i
			break
		}
	}
	if snapI < 0 {
		return errors.New("snapshot not found")
	}
	d.snapshots[volumeUUID] = append(snaps[:snapI], snaps[snapI+1:]...)
	return nil
}

type fakeDiskUtil struct {
	devices *fakeDevices
}

func (du *fakeDiskUtil) Info(volume string) (diskutil.VolumeInfo, error) {
	return du.devices.Volume(volume)
}

func (du *fakeDiskUtil) Rename(volume diskutil.VolumeInfo, name string) error {
	snaps, err := du.devices.Snapshots(volume.UUID)
	if err != nil {
		return err
	}

	if err := du.devices.RemoveVolume(volume.UUID); err != nil {
		return err
	}
	volume.Name = name
	return du.devices.AddVolume(volume, snaps...)
}

func (du *fakeDiskUtil) ListSnapshots(volume diskutil.VolumeInfo) ([]diskutil.Snapshot, error) {
	return du.devices.Snapshots(volume.UUID)
}

func (du *fakeDiskUtil) DeleteSnapshot(volume diskutil.VolumeInfo, snap diskutil.Snapshot) error {
	return du.devices.DeleteSnapshot(volume.UUID, snap.UUID)
}

type readonlyFakeDiskUtil struct {
	du *fakeDiskUtil

	diskutil.DiskUtil
}

func (du *readonlyFakeDiskUtil) Info(volume string) (diskutil.VolumeInfo, error) {
	return du.du.Info(volume)
}

func (du *readonlyFakeDiskUtil) ListSnapshots(volume diskutil.VolumeInfo) ([]diskutil.Snapshot, error) {
	return du.du.ListSnapshots(volume)
}

type fakeASR struct {
	devices *fakeDevices
}

func (asr *fakeASR) Restore(source, target diskutil.VolumeInfo, to, from diskutil.Snapshot) error {
	// Validate source and target volumes exist.
	if _, err := asr.devices.Volume(source.UUID); err != nil {
		return err
	}
	if _, err := asr.devices.Volume(target.UUID); err != nil {
		return err
	}

	// Validate that `from` exists in both source and target.
	if _, err := asr.devices.Snapshot(source.UUID, from.UUID); err != nil {
		return err
	}
	if _, err := asr.devices.Snapshot(target.UUID, from.UUID); err != nil {
		return err
	}
	// Valdiate that `to` exists in source.
	_, err := asr.devices.Snapshot(source.UUID, to.UUID)
	if err != nil {
		return err
	}
	// Add `to` snapshot to target.
	if err := asr.devices.AddSnapshot(target.UUID, to); err != nil {
		return err
	}
	// Rename target to source name.
	snaps, err := asr.devices.Snapshots(target.UUID)
	if err != nil {
		return err
	}
	if err := asr.devices.RemoveVolume(target.UUID); err != nil {
		return err
	}
	target.Name = source.Name
	return asr.devices.AddVolume(target, snaps...)
}

func (asr *fakeASR) DestructiveRestore(source, target diskutil.VolumeInfo, to diskutil.Snapshot) error {
	// Validate source and target volumes exist.
	if _, err := asr.devices.Volume(source.UUID); err != nil {
		return err
	}
	if _, err := asr.devices.Volume(target.UUID); err != nil {
		return err
	}
	// Valdiate that `to` exists in source.
	_, err := asr.devices.Snapshot(source.UUID, to.UUID)
	if err != nil {
		return err
	}
	// Remove target volume to "erase" it.
	if err := asr.devices.RemoveVolume(target.UUID); err != nil {
		return err
	}
	// Add back the target volume, renamed to source name, and with the
	// single `to` snapshot.
	target.Name = source.Name
	return asr.devices.AddVolume(target, to)
}
