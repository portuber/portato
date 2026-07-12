// Package licensetext exposes the project's canonical MIT LICENSE text so the
// `portato license --full` command (internal/cmd) can print it without the
// repo on disk. The embed directive lives here — at the module root, next to
// LICENSE — because //go:embed patterns are relative to the source file and
// cannot ascend with "..", so no subdirectory package could reach the root
// LICENSE without keeping a diverging copy.
package licensetext

import _ "embed"

// MIT is the verbatim text of the project's MIT License (the LICENSE file at
// the module root). It is the single embedded source for `portato license
// --full`, so the on-disk file and the binary never drift.
//
//go:embed LICENSE
var MIT string
