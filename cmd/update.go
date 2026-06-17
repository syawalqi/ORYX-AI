package cmd

import (
	"github.com/syawalqi/oryx/updatepkg"
)

// Update performs a self-update. If force is true, it downloads even if
// already up to date. The track parameter overrides the saved install track.
func Update(currentVersion string, force bool, trackOverride string) error {
	if trackOverride != "" {
		return updatepkg.RunWithTrack(currentVersion, updatepkg.Track(trackOverride), force)
	}
	return updatepkg.Run(currentVersion, force)
}
