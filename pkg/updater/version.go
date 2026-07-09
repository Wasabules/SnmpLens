package updater

// Version is the running application version. It is injected at build time via
// ldflags (see .github/workflows/release.yml):
//
//	-ldflags "-X SnmpLens/pkg/updater.Version=v1.2.3"
//
// Local / dev builds keep the default "dev", which disables update checks so the
// developer never gets a spurious "update available" prompt.
var Version = "dev"

// isDevVersion reports whether the current build is an untagged local/dev build.
func isDevVersion() bool {
	return Version == "" || Version == "dev"
}
