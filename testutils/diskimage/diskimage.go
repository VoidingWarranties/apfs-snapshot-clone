// +build darwin

package diskimage

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
	"os/exec"
	"path/filepath"

	"apfs-snapshot-diff-clone/diskutil"
	"apfs-snapshot-diff-clone/plutil"
)

const (
	SourceImg = "testdata/source.dmg"
	TargetImg = "testdata/target.dmg"
)

var (
	SourceInfo = diskutil.VolumeInfo{
		Name: "source",
		UUID: "CA79DDFA-D75D-43F3-8099-3BEA2F7C1F33",
	}
	SourceSnaps = [...]diskutil.Snapshot{
		{
			Name:    "com.bombich.ccc.6AE4815C-1F9A-4D5E-86E1-19078BE01958.2021-03-01-203509",
			UUID:    "D1ABE254-5B1B-4FDF-8DB3-1B4B4B825E39",
			Created: time.Date(2021, 3, 1, 20, 35, 9, 0, time.UTC),
		},
		{
			Name:    "com.bombich.ccc.D7B2D286-3CE0-40B9-9797-EBF108ADAD30.2021-03-01-203433",
			UUID:    "A175CCCF-0C56-4A46-97FB-CA267A540C96",
			Created: time.Date(2021, 3, 1, 20, 34, 33, 0, time.UTC),
		},
	}

	TargetInfo = diskutil.VolumeInfo{
		Name: "target",
		UUID: "21CF5985-FA46-42AF-9872-52CDE74B04DE",
	}
	TargetSnaps = [...]diskutil.Snapshot{
		{
			Name:    "com.bombich.ccc.D7B2D286-3CE0-40B9-9797-EBF108ADAD30.2021-03-01-203433",
			UUID:    "A175CCCF-0C56-4A46-97FB-CA267A540C96",
			Created: time.Date(2021, 3, 1, 20, 34, 33, 0, time.UTC),
		},
	}
)

// MountRO mounts the disk image at `path` as a readonly volume and
// returns the mount point.
func MountRO(t *testing.T, path string) (mountpoint, device string) {
	t.Helper()

	mountpoint = t.TempDir()
	cmd := exec.Command(
		"hdiutil", "attach",
		// There's an odd bug in MacOS where repeatedly calling
		// `hdiutil attach` and `hdiutil detach` on an image and it's
		// volume will cause Finder to sometimes display multiple
		// Macintosh HD volumes. The -nobrowse flag seems to prevent
		// the visible symptoms of this bug, but this could also just
		// be hiding weirdness.
		"-nobrowse",
		"-plist",
		"-readonly",
		"-mountpoint", mountpoint,
		path,
	)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	// TODO: it would be nice if *exec.ExitError.Error() included the stderr, if any.
	if err != nil {
		t.Fatalf("failed to mount %q (%v): %s", path, err, stderr)
	}
	// Mount point may have changed by the time we cleanup (e.g. by `asr
	// restore`). Get the the device node to use during cleanup.
	device, err = parseHdiutilAttachOutput(stdout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd := exec.Command("hdiutil", "detach", "-force", device)
		err := cmd.Run()
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("failed to unmount %q: %v: %s", device, err, exitErr.Stderr)
		}
		if err != nil {
			t.Fatalf("failed to unmount %q: %v", device, err)
		}
	})
	// t.TempDir can return a path that contains a symlink. Evaluate the
	// mount point, as `diskutil info` returns the non-symlink mount
	// points. We could return `info.MountPoint`, but then
	// diskutil_darwin_test's TestInfo wouldn't be truly testing the
	// MountPoint value.
	mountpoint, err = filepath.EvalSymlinks(mountpoint)
	if err != nil {
		t.Fatal(err)
	}
	return mountpoint, device
}

// MountRW mounts the disk image at `path` as a read/write volume
// using a shadow file. All modifications to the volume are written to
// the shadow file rather than the disk image.
func MountRW(t *testing.T, path string) (mountpoint, device string) {
	t.Helper()

	mountpoint = t.TempDir()
	shadow := filepath.Join(t.TempDir(), "shadow")
	cmd := exec.Command(
		"hdiutil", "attach",
		"-nobrowse",
		"-plist",
		"-shadow", shadow,
		"-mountpoint", mountpoint,
		path,
	)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	// TODO: it would be nice if *exec.ExitError.Error() included the stderr, if any.
	if err != nil {
		t.Fatalf("failed to mount %q (%v): %s", path, err, stderr)
	}
	device, err = parseHdiutilAttachOutput(stdout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd := exec.Command("hdiutil", "detach", "-force", device)
		err := cmd.Run()
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("failed to unmount %q: %v: %s", device, err, exitErr.Stderr)
		}
		if err != nil {
			t.Fatalf("failed to unmount %q: %v", device, err)
		}
	})
	mountpoint, err = filepath.EvalSymlinks(mountpoint)
	if err != nil {
		t.Fatal(err)
	}
	return mountpoint, device
}

func parseHdiutilAttachOutput(r io.Reader) (device string, err error) {
	pl := plutil.New()
	var info struct{
		SystemEntities []struct{
			MountPoint string `json:"mount-point"`
			DevEntry   string `json:"dev-entry"`
		} `json:"system-entities"`
	}
	if err := pl.DecodePlist(r, &info); err != nil {
		return "", err
	}
	found := false
	for _, e := range info.SystemEntities {
		if e.MountPoint == "" {
			continue
		}
		if found {
			return "", errors.New("diskimage test utility only supports images with a single volume")
		}

		found = true
		device = e.DevEntry
	}
	return device, nil
}
