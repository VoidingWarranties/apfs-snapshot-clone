package diskutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"time"
)

var execCommand = exec.Command

type DiskUtil struct {}

func (d DiskUtil) Rename(volume string, name string) error {
	cmd := execCommand("diskutil", "rename", volume, name)
	cmd.Stdout = os.Stdout
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr

	log.Printf("Running command:\n%s", cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`%s` failed (%v) with stderr: %s", cmd, err, stderr)
	}
	return nil
}

type VolumeInfo struct {
	UUID       string `json:"VolumeUUID"`
	Name       string `json:"VolumeName"`
	MountPoint string `json:"MountPoint"`
}

func (d DiskUtil) Info(volume string) (VolumeInfo, error) {
	cmd := execCommand("diskutil", "info", "-plist", volume)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return VolumeInfo{}, fmt.Errorf("error creating stdout pipe: %v", err)
	}
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	log.Printf("Running command:\n%s", cmd)
	if err := cmd.Start(); err != nil {
		return VolumeInfo{}, fmt.Errorf("`%s` failed to start: %v", cmd, err)
	}
	var info VolumeInfo
	if err := decodePlist(stdout, &info); err != nil {
		return VolumeInfo{}, fmt.Errorf("error parsing plist: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		return VolumeInfo{}, fmt.Errorf("`%s` failed (%v) with stderr: %s", cmd, err, stderr)
	}
	return info, nil
}

type Snapshot struct {
	Name    string    `json:"SnapshotName"`
	UUID    string    `json:"SnapshotUUID"`
	Created time.Time `json:"-"`
}

func (s Snapshot) String() string {
	return fmt.Sprintf("%s (%s)", s.Name, s.UUID)
}

func (d DiskUtil) ListSnapshots(volume string) ([]Snapshot, error) {
	cmd := execCommand("diskutil", "apfs", "listsnapshots", "-plist", volume)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("error creating stdout pipe: %v", err)
	}
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	log.Printf("Running command:\n%s", cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("`%s` failed to start: %v", cmd, err)
	}
	var snapshotList struct {
		Snapshots []Snapshot `json:"Snapshots"`
	}
	if err := decodePlist(stdout, &snapshotList); err != nil {
		return nil, fmt.Errorf("error parsing plist: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("`%s` failed (%v) with stderr: %s", cmd, err, stderr)
	}

	// TODO: document why we sort here.
	snapshots := snapshotList.Snapshots
	isSorted := sort.SliceIsSorted(snapshots, func(i, ii int) bool {
		lhs := snapshots[i]
		rhs := snapshots[ii]
		return lhs.Created.Before(rhs.Created)
	})
	if !isSorted {
		return nil, fmt.Errorf("`%s` returned snapshots in an unexpected order", cmd)
	}
	for i, ii := 0, len(snapshots)-1; i < ii; i, ii = i+1, ii-1 {
		snapshots[i], snapshots[ii] = snapshots[ii], snapshots[i]
	}
	return snapshots, nil
}

func parseTimeFromSnapshotName(name string) (time.Time, error) {
	timeRegex := regexp.MustCompile(`\d{4}-\d{2}-\d{2}-\d{6}`)
	timeMatch := timeRegex.FindString(name)
	if len(timeMatch) == 0 {
		return time.Time{}, fmt.Errorf("snapshot name (%q) does not contain a timestamp of the form yyyy-mm-dd-hhmmss", name)
	}
	created, err := time.Parse("2006-01-02-150405", string(timeMatch))
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse time substring (%q) from snapshot name", timeMatch)
	}
	return created, nil
}

func (d DiskUtil) DeleteSnapshot(volume string, snap Snapshot) error {
	cmd := execCommand("diskutil", "apfs", "deletesnapshot", volume, "-uuid", snap.UUID)
	cmd.Stdout = os.Stdout
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr

	log.Printf("Running command:\n%s", cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`%s` failed (%v) with stderr: %s", cmd, err, stderr)
	}
	return nil
}

func decodePlist(r io.Reader, v interface{}) error {
	cmd := execCommand(
		"plutil",
		"-convert", "json",
		// Read from stdin.
		"-",
		// Output to stdout.
		"-o", "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error creating stdout pipe: %v", err)
	}
	cmd.Stdin = r
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("`%s` failed to start: %v", cmd, err)
	}
	if err := json.NewDecoder(stdout).Decode(v); err != nil {
		return fmt.Errorf("failed to parse json: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("`%s` failed (%w) with stderr: %s", cmd, err, stderr)
	}
	return nil
}
