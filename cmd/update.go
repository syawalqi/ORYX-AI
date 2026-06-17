package cmd

import (
	"github.com/syawalqi/oryx/updatepkg"
)

// Update delegates to the shared update logic.
func Update(currentVersion string) error {
	return updatepkg.Run(currentVersion)
}
