// Package version holds the canonical application version string.
// The build script stamps AppVersion via -ldflags at release time;
// the fallback "0.0.0-dev" is used for local/CI builds without a tag.
package version

var AppVersion = "0.0.0-dev"
