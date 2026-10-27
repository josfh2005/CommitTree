//go:build !dev

package update

// DevBuild is true in a Wails dev build (`-tags dev`), which never installs
// updates in place.
const DevBuild = false
